package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

func createUser(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		log.Printf("JSON decoding error: %v\n", err)
		sendJSONResponse(w, http.StatusBadRequest, Response{Status: "failed", Data: err.Error()})
		return
	}

	name := r.FormValue("user_name")
	email := r.FormValue("user_email")
	password := r.FormValue("user_password")
	if name == "" || email == "" || password == "" {
		fmt.Println("Form empty error")
		sendJSONResponse(w, http.StatusBadRequest, Response{Status: "failed", Data: nil})
		return
	}
	// Encrypt the password with bcrypt
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Println("Password hashing error:", err.Error())
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "failed", Data: nil})
		return
	}
	fmt.Println(name)
	fmt.Println(email)
	fmt.Println(password)
	fmt.Println(hashedPassword)

	db, err := dbConnect(w)
	if err != nil {
		fmt.Println("dbconnect error:", err.Error())
		return
	}

	stmtCount, err := db.Prepare("SELECT COUNT(*) FROM user WHERE user_email = ?")
	if err != nil {
		fmt.Println("SQL Prepare error:", err.Error())
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "failed", Data: err})
		return
	}
	defer stmtCount.Close()

	var count int
	err = stmtCount.QueryRow(email).Scan(&count)
	if err != nil {
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "failed", Data: nil})
		return
	}
	if count > 0 { // Email already exists, handle the error
		sendJSONResponse(w, http.StatusBadRequest, Response{Status: "failed", Data: "Email already exist"})
		return
	}

	otp := generateOTP()

	stmt, err := db.Prepare("INSERT INTO user (user_name, user_email, user_password) VALUES (?, ?, ?)")
	if err != nil {
		fmt.Println("SQL Prepare error:", err.Error())
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "prepare failed", Data: nil})
		return
	}
	defer stmt.Close()
	_, err = stmt.Exec(name, email, hashedPassword)
	if err != nil {
		fmt.Println("Execute error:", err.Error())
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "Execute failed", Data: nil})
		return
	}
	sendMail(w, otp, email)

	sendJSONResponse(w, http.StatusOK, Response{Status: "success", Data: nil})
}

func generateOTP() int {
	return rand.Intn(900000) + 100000
}

func sendMail(w http.ResponseWriter, otp int, to string) {
	otpStr := strconv.Itoa(otp)
	payload := map[string]interface{}{
		"otp":   otpStr,
		"email": to,
	}

	jsonPayload, err := json.Marshal(payload) //same like stringify
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	log.Println("Payload to be sent:", string(jsonPayload))
	resp, err := http.Post("https://script.google.com/macros/s/AKfycbzBp3LargzIzRtS8yie8tXSDdE9NtZEcjEwfT60gjilLY6nBAti89fXRLAgeoOnUo4acg/exec", "application/json", bytes.NewBuffer(jsonPayload))
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
	fmt.Println("Email sent successfully")
}
