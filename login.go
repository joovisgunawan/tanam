package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/dgrijalva/jwt-go"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

func loginHandler(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		log.Printf("Parsing form error: %v\n", err)
		sendJSONResponse(w, http.StatusBadRequest, Response{Status: "failed", Data: err.Error()})
		return
	}

	email := r.FormValue("user_email")
	password := r.FormValue("user_password")
	fmt.Println(email)
	fmt.Println(password)
	if email == "" || password == "" {
		fmt.Println("Form empty error")
		sendJSONResponse(w, http.StatusBadRequest, Response{Status: "failed", Data: nil})
		return
	}
	err = incrementAttempts(email)
	if err != nil {
		log.Printf("Failed to increment login attempts: %v", err)
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "failed", Data: "Internal Server Error"})
		// http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	attemptsKey := fmt.Sprintf("login_attempts:%s", email)
	val, err := rdb.Get(context.Background(), attemptsKey).Result()
	if err != nil && err != redis.Nil {
		log.Printf("Failed to retrieve login attempts: %v", err)
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "failed", Data: "Internal Server Error"})
		// http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	var attempts LoginAttempts
	if err := json.Unmarshal([]byte(val), &attempts); err != nil {
		attempts = LoginAttempts{} // Initialize if no previous attempts
	}

	// Check if max attempts exceeded
	if attempts.Count >= 5 {
		sendJSONResponse(w, http.StatusTooManyRequests, Response{Status: "failed", Data: "Maximum login attempts exceeded"})
		// http.Error(w, "Maximum login attempts exceeded", http.StatusTooManyRequests)
		return
	}

	db, err := dbConnect(w)
	if err != nil {
		fmt.Println("dbconnect error:", err.Error())
		return
	}
	defer db.Close()

	passStmt, err := db.Prepare("SELECT user_password FROM user WHERE user_email = ?")
	if err != nil {
		fmt.Println("SQL Prepare password error:", err.Error())
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "failed", Data: nil})
		return
	}
	defer passStmt.Close()

	var hashedPassword string
	err = passStmt.QueryRow(email).Scan(&hashedPassword)
	if err != nil {
		if err == sql.ErrNoRows {
			sendJSONResponse(w, http.StatusUnauthorized, Response{Status: "failed", Data: "Invalid email or password"})
			return
		}
		log.Println("Error fetching hashed password:", err.Error())
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "failed", Data: "Database error"})
		return
	}

	// Verify password
	err = bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	if err != nil {
		sendJSONResponse(w, http.StatusUnauthorized, Response{Status: "failed", Data: "Invalid email or password"})
		return
	}

	stmt, err := db.Prepare("SELECT user_id, user_email, user_password, user_name FROM user WHERE user_email = ? AND user_password = ?")
	if err != nil {
		fmt.Println("SQL Prepare error:", err.Error())
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "failed", Data: nil})
		return
	}
	defer stmt.Close()

	rows, err := stmt.Query(email, hashedPassword)
	if err != nil {
		fmt.Println("SQL Query error:", err.Error())
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "failed", Data: nil})
		return
	}
	defer rows.Close()

	if !rows.Next() {
		sendJSONResponse(w, http.StatusUnauthorized, Response{Status: "failed", Data: "user not found"})
		return
	}

	var fetchedUser User
	err = rows.Scan(&fetchedUser.ID, &fetchedUser.Email, &fetchedUser.Password, &fetchedUser.Name)
	if err != nil {
		fmt.Println("Fetching error:", err.Error())
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "failed", Data: nil})
		return
	}

	resetAttempts(email)

	tokenString, expirationTime, err := createToken(email, 5*time.Minute)
	if err != nil {
		fmt.Println("Create access token error:", err.Error())
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	refreshTokenString, refreshExpirationTime, err := createToken(email, 7*24*time.Hour)
	if err != nil {
		fmt.Println("Create refresh token error:", err.Error())
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:    "access_token",
		Value:   tokenString,
		Expires: expirationTime,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    refreshTokenString,
		Expires:  refreshExpirationTime,
		HttpOnly: true,
	})

	sendJSONResponse(w, http.StatusOK, Response{Status: "success", Data: fetchedUser})
}

func incrementAttempts(email string) error {
	attemptsKey := fmt.Sprintf("login_attempts:%s", email)
	val, err := rdb.Get(context.Background(), attemptsKey).Result()
	if err != nil && err != redis.Nil {
		return err
	}

	var attempts LoginAttempts
	if err := json.Unmarshal([]byte(val), &attempts); err != nil {
		attempts = LoginAttempts{} // Initialize if no previous attempts
	}

	attempts.Count++
	attempts.LastAttempt = time.Now()

	// Store updated attempts back in Redis
	data, err := json.Marshal(attempts)
	if err != nil {
		return err
	}

	return rdb.Set(context.Background(), attemptsKey, string(data), 10*time.Minute).Err()
}

func resetAttempts(email string) {
	attemptsKey := fmt.Sprintf("login_attempts:%s", email)
	err := rdb.Del(context.Background(), attemptsKey).Err()
	if err != nil {
		log.Printf("Failed to delete attempts for userID %s: %v", email, err)
		// Handle the error as needed (logging, retrying, etc.)
	}
}

func createToken(email string, duration time.Duration) (string, time.Time, error) {
	expirationTime := time.Now().Add(duration)
	claims := &Claims{
		Email: email,
		StandardClaims: jwt.StandardClaims{
			ExpiresAt: expirationTime.Unix(),
			IssuedAt:  time.Now().Unix(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtKey)
	return tokenString, expirationTime, err

}

func refreshHandler(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("refresh_token")
	if err != nil {
		if err == http.ErrNoCookie {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	refreshTokenStr := c.Value
	claims := &Claims{}
	tkn, err := jwt.ParseWithClaims(refreshTokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return jwtKey, nil
	})

	if err != nil {
		if err == jwt.ErrSignatureInvalid {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	if !tkn.Valid {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Check if the refresh token is expired
	if time.Unix(claims.ExpiresAt, 0).Before(time.Now()) {
		http.Error(w, "Unauthorized - Refresh token expired", http.StatusUnauthorized)
		return
	}

	tokenString, accessExpirationTime, err := createToken(claims.Email, 5*time.Minute)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	//set cookie for access token
	http.SetCookie(w, &http.Cookie{
		Name:     "access_token",
		Value:    tokenString,
		Expires:  accessExpirationTime,
		HttpOnly: true,
	})

	sendJSONResponse(w, http.StatusOK, Response{Status: "success", Data: "Token refreshed"})
}

func forgotPassword(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		log.Printf("Parsing json error: %v\n", err)
		sendJSONResponse(w, http.StatusBadRequest, Response{Status: "failed", Data: err.Error()})
		return
	}

	email := req["user_email"]
	if email == "" {
		fmt.Println("json empty error")
		sendJSONResponse(w, http.StatusBadRequest, Response{Status: "failed", Data: nil})
		return
	}
	fmt.Println(email)

	if err != nil {
		fmt.Println("Encryption error:", err)
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "error", Data: err.Error()})
		return
	}

	payload := map[string]interface{}{
		"url":   "https://www.tanam.software/reset/resetpassword.php?email=" + email,
		"email": email,
	}

	jsonPayload, err := json.Marshal(payload) //same like stringify
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	log.Println("Payload to be sent:", string(jsonPayload))
	resp, err := http.Post("https://script.google.com/macros/s/AKfycbwr6PYtoMC5idZeuaVqWSq0ScPB5-htIYmAuhlzDOz7cydA8MW0rBwLf3d29-LL8hzH/exec", "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		fmt.Println("Error:", err)
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "error", Data: err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Println("Error: Failed to send data to the API")
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "error", Data: resp.StatusCode})
		return
	}
	sendJSONResponse(w, http.StatusOK, Response{
		Status: "success",
		Data:   nil,
	})
	fmt.Println("Email sent successfully")
}
