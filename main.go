package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/redis/go-redis/v9"
)

type Response struct {
	TotalPages int         `json:"totalPage,omitempty"` // Optional field
	Status     string      `json:"status"`
	Data       interface{} `json:"data"`
}

type CachedResponse struct {
	Products   []Product `json:"products"`
	TotalPages int       `json:"totalPages"`
}

type User struct {
	ID       string `json:"user_id"`
	Name     string `json:"user_name"`
	Email    string `json:"user_email"`
	Password string `json:"user_password"`
	Gender   string `json:"User_gender"`
	Phone    int    `json:"user_phone"`
	Address  string `json:"user_address"`
	Photo    string `json:"user_photo"`
}

type RequestParams struct {
	CurrentPage     int    `json:"current_page,omitempty"`
	UserID          string `json:"user_id,omitempty"`
	SearchKey       string `json:"search_key,omitempty"`
	ProductCategory string `json:"product_category,omitempty"`
}

type Product struct {
	ProductID          int    `json:"product_id"`
	ProductName        string `json:"product_name"`
	ProductCategory    string `json:"product_category"`
	ProductPrice       string `json:"product_price"` //if it is set tp float64, dart sometimes read it as int or double, if no decimal, so int, if decimal so double
	ProductQuantity    int    `json:"product_quantity"`
	ProductState       string `json:"product_state"`
	ProductDescription string `json:"product_description"`
	SellerID           int    `json:"seller_id"`
	ProductImageUrl    string `json:"product_image_url"`
}

type Category struct {
	CategoryID   string `json:"category_id"`
	CategoryName string `json:"category_name"`
}

type Claims struct {
	Email string `json:"email"`
	jwt.StandardClaims
}

type gzipResponseWriter struct {
	http.ResponseWriter
	*gzip.Writer
}

type LoginAttempts struct {
	Count       int       // Number of attempts
	LastAttempt time.Time // Last attempt time
}

var jwtKey = []byte("tanam_api_key")

var tanamApiKey = []byte("tanam_api_key")

var rdb = redis.NewClient(&redis.Options{
	Addr:     "localhost:6379",
	Password: "",
	DB:       0,
})

func main() {
	httpsMux := http.NewServeMux()
	httpsMux.HandleFunc("/api/tanam/login", loginHandler)
	httpsMux.HandleFunc("/login", loginHandler)
	httpsMux.HandleFunc("/register", createUser)
	// httpsMux.HandleFunc("/upload", IsAuthorized(uploadFile))
	// httpsMux.HandleFunc("/uploads/", serveImage)
	// httpsMux.HandleFunc("/cart", IsAuthorized(serveImage))
	httpsMux.HandleFunc("/refresh", refreshHandler)
	httpsMux.HandleFunc("/getProduct", getProduct)
	// httpsMux.HandleFunc("/getCategory", getCategory)

	fmt.Println("Starting HTTP server on port 8081 for redirection to HTTPS")
	if err := http.ListenAndServe(":8081", httpsMux); err != nil {
		log.Fatalf("HTTP server failed to start: %v", err)
	}
}

func main2() {
	httpsMux := http.NewServeMux()

	registerMidHandler := ChainMiddleware(http.HandlerFunc(createUser), LoggingMiddleware, APIKeyMiddleware, GzipMiddleware)
	httpsMux.Handle("/api/tanam/register", registerMidHandler)

	loginMidHandler := ChainMiddleware(
		http.HandlerFunc(loginHandler),
		LoggingMiddleware,
		// MaxLoginAttemptsMiddleware,
		APIKeyMiddleware,
		GzipMiddleware,
	)
	httpsMux.Handle("/api/tanam/login", loginMidHandler)
	httpsMux.HandleFunc("/api/tanam/login2", loginHandler)
	httpsMux.HandleFunc("/login", loginHandler)

	insertProductMidHandler := ChainMiddleware(http.HandlerFunc(insertProduct), LoggingMiddleware, APIKeyMiddleware, JWTMiddleware, GzipMiddleware)
	httpsMux.Handle("/api/tanam/insertproduct", insertProductMidHandler)

	getProductMidHandler := ChainMiddleware(http.HandlerFunc(getProduct), LoggingMiddleware, APIKeyMiddleware, GzipMiddleware)
	httpsMux.Handle("/api/tanam/getproduct", getProductMidHandler)

	loadImageMidHandler := ChainMiddleware(http.HandlerFunc(loadImage), LoggingMiddleware)
	httpsMux.Handle("/api/tanam/loadimage/", loadImageMidHandler)

	forgotMidHandler := ChainMiddleware(http.HandlerFunc(forgotPassword), LoggingMiddleware, APIKeyMiddleware, GzipMiddleware)
	httpsMux.Handle("/api/tanam/forgotpassword", forgotMidHandler)

	refreshMidHandler := ChainMiddleware(http.HandlerFunc(refreshHandler), LoggingMiddleware, APIKeyMiddleware, GzipMiddleware)
	httpsMux.Handle("/api/tanam/refresh", refreshMidHandler)

	certFile := "/etc/letsencrypt/live/api.tanam.software/fullchain.pem"
	keyFile := "/etc/letsencrypt/live/api.tanam.software/privkey.pem"

	go func() {
		fmt.Println("Starting HTTPS server on port 8488")
		err := http.ListenAndServeTLS(":8488", certFile, keyFile, httpsMux)
		if err != nil {
			log.Fatalf("HTTPS server failed to start: %v", err)
		}
	}()

	httpMux := http.NewServeMux()
	// httpMux.HandleFunc("/", redirect)
	httpMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		host := strings.Split(r.Host, ":")[0]
		fmt.Println(host)
		fmt.Println(r.RequestURI)
		http.Redirect(w, r, "https://"+host+":8488"+r.RequestURI, http.StatusTemporaryRedirect)
	})
	// When you perform a redirect in Go using http.Redirect,
	//  the default behavior is to redirect with a GET request.
	// This is part of the HTTP specification where redirects typically use GET requests to navigate to the new location.
	//Use 307 Temporary Redirect dont use 301 http.StatusMovedPermanently
	// HTTP status code 307 (Temporary Redirect) can be used to indicate that the request should be repeated with the same HTTP method.
	// In your Go code, you can set this status code explicitly:
	fmt.Println("Starting HTTP server on port 8081 for redirection to HTTPS")
	err := http.ListenAndServe(":8081", httpMux)
	if err != nil {
		log.Fatalf("HTTP server failed to start: %v", err)
	}
}

// func redirect(w http.ResponseWriter, r *http.Request) {
// 	host := strings.Split(r.Host, ":")[0]
// 	http.Redirect(w, r, "https://"+host+":8488"+r.RequestURI, http.StatusMovedPermanently)
// }

func sendJSONResponse(w http.ResponseWriter, status int, responseData interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(responseData)
}

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("method=%s uri=%s duration=%s", r.Method, r.RequestURI, time.Since(start))
	})
}

func GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			gz := gzip.NewWriter(w)
			defer gz.Close()

			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Vary", "Accept-Encoding")

			gzw := gzipResponseWriter{Writer: gz, ResponseWriter: w}
			next.ServeHTTP(gzw, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func APIKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for API key in the request headers
		apiKey := r.Header.Get("X-API-Key")
		if apiKey != string(tanamApiKey) {
			fmt.Println("API key Unauthorized")
			http.Error(w, "Forbidden: Invalid API Key", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func JWTMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("access_token")
		if err != nil {
			if err == http.ErrNoCookie {
				fmt.Println("JWT Unauthorized", err.Error())
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		tokenStr := c.Value
		claims := &Claims{}
		tkn, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
			return jwtKey, nil
		})

		if err != nil {
			if err == jwt.ErrSignatureInvalid {
				fmt.Println("JWT Unauthorized", err.Error())
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		if !tkn.Valid {
			fmt.Println("Token not valid")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Override the Write method to use gzip.Writer
func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

// Override the Header method to use the original ResponseWriter's Header
func (w gzipResponseWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

// Override the WriteHeader method to use the original ResponseWriter's WriteHeader
func (w gzipResponseWriter) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode)
}

func ChainMiddleware(handler http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for _, middleware := range middlewares {
		handler = middleware(handler)
	}
	return handler
}
