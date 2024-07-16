package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

func insertProduct(w http.ResponseWriter, r *http.Request) {
	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		log.Printf("Headers: %v\n", r.Header)
		log.Printf("Error parsing multipart form: %v\n", err)
		sendJSONResponse(w, http.StatusBadRequest, Response{Status: "failed", Data: err.Error()})
		return
	}

	product_image, handler, err := r.FormFile("product_image")
	if err != nil {
		log.Printf("Error retrieving the image file: %v\n", err)
		http.Error(w, "Error retrieving the image file", http.StatusBadRequest)
		return
	}
	defer product_image.Close()

	product_name := r.FormValue("product_name")
	product_category := r.FormValue("product_category")
	product_price := r.FormValue("product_price")
	product_quantity := r.FormValue("product_quantity")
	product_state := r.FormValue("product_state")
	product_description := r.FormValue("product_description")
	seller_id := r.FormValue("seller_id")

	price, err := strconv.ParseFloat(product_price, 64)
	if err != nil {
		http.Error(w, "Invalid product price", http.StatusBadRequest)
		return
	}
	quantity, err := strconv.Atoi(product_quantity)
	if err != nil {
		http.Error(w, "Invalid product quantity", http.StatusBadRequest)
		return
	}
	fmt.Println(product_name)
	fmt.Println(product_category)
	fmt.Println(product_price)
	fmt.Println(price)
	fmt.Println(product_quantity)
	fmt.Println(quantity)
	fmt.Println(product_state)
	fmt.Println(product_description)
	fmt.Println(seller_id)

	if product_name == "" || product_category == "" || product_price == "" || product_quantity == "" || product_state == "" || product_description == "" || seller_id == "" {
		fmt.Println("Form empty error")
		sendJSONResponse(w, http.StatusBadRequest, Response{Status: "failed", Data: nil})
		return
	}

	fileExtension := filepath.Ext(handler.Filename)
	if fileExtension == "" {
		log.Printf("Image file extension error")
		http.Error(w, "Image File extension is missing", http.StatusBadRequest)
		return
	}

	err = os.MkdirAll("uploads", os.ModePerm)
	if err != nil {
		log.Printf("Error creating 'catches' directory: %v\n", err)
		http.Error(w, "Failed to create 'catches' directory", http.StatusInternalServerError)
		return
	}

	newFileName := fmt.Sprintf("%s%d%s", product_name, time.Now().UnixNano(), fileExtension)
	newFileName = filepath.Base(newFileName)
	destinationPath := filepath.Join("uploads", newFileName)
	url := fmt.Sprintf("https://api.tanam.software:8488/api/tanam/loadimage/%s", destinationPath)

	newFile, err := os.Create(destinationPath)
	if err != nil {
		log.Printf("Error creating the new file indestination path: %v\n", err)
		http.Error(w, "Error creating the file in destination path", http.StatusInternalServerError)
		return
	}
	defer newFile.Close()

	_, err = io.Copy(newFile, product_image)
	if err != nil {
		log.Printf("Error copying the file: %v\n", err)
		http.Error(w, "Error copying the file", http.StatusInternalServerError)
		return
	}

	log.Printf("File uploaded successfully: %s\n", newFileName)
	db, err := dbConnect(w)
	if err != nil {
		return
	}

	stmt, err := db.Prepare("INSERT INTO product (product_name, product_category, product_price, product_quantity, product_state, product_description, seller_id, product_image_url) VALUES (?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		log.Printf("Error preparing SQL statement: %v\n", err)
		http.Error(w, "Error preparing SQL statement", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	_, err = stmt.Exec(product_name, product_category, price, quantity, product_state, product_description, seller_id, url)
	if err != nil {
		log.Printf("Error executing SQL statement: %v\n", err)
		http.Error(w, "Error executing SQL statement", http.StatusInternalServerError)
		return
	}
	sendJSONResponse(w, http.StatusOK, Response{Status: "success", Data: "Product Inserted"})
}

func getProduct(w http.ResponseWriter, r *http.Request) {
	var params RequestParams
	err := json.NewDecoder(r.Body).Decode(&params)
	if err != nil {
		fmt.Println("Failed to parse request body", err.Error())
		http.Error(w, "Failed to parse request body", http.StatusBadRequest)
		return
	}

	conditions := ""
	args := []interface{}{}

	if params.UserID != "" {
		conditions += " WHERE seller_id = ?"
		args = append(args, params.UserID)
	} else if params.SearchKey != "" {
		conditions += " WHERE product_name LIKE ?"
		args = append(args, "%"+params.SearchKey+"%")
	} else if params.ProductCategory != "" {
		if params.ProductCategory == "For You" {
			// No additional condition
		} else {
			conditions += " WHERE product_category LIKE ?"
			args = append(args, "%"+params.ProductCategory+"%")
		}
	}

	cacheKey := fmt.Sprintf("products:%s:%s:%s:%d", params.UserID, params.SearchKey, params.ProductCategory, params.CurrentPage)
	ctx := context.Background()

	cachedProducts, err := rdb.Get(ctx, cacheKey).Result()
	if err == redis.Nil {
		// Cache miss, query the database
		db, err := dbConnect(w)
		if err != nil {
			fmt.Println("dbconnect error:", err.Error())
			return
		}
		defer db.Close()

		totalCountStmt, err := db.Prepare("SELECT COUNT(*) FROM product" + conditions)
		if err != nil {
			fmt.Println("Failed to prepare count statement", err.Error())
			http.Error(w, "Failed to prepare count statement", http.StatusInternalServerError)
			return
		}
		defer totalCountStmt.Close()

		var totalResult int
		err = totalCountStmt.QueryRow(args...).Scan(&totalResult)
		if err != nil {
			fmt.Println("Failed to fetch total count", err.Error())
			http.Error(w, "Failed to fetch total count", http.StatusInternalServerError)
			return
		}

		currentPage := 1
		if params.CurrentPage > 0 {
			currentPage = params.CurrentPage
		}

		resultPerPage := 6
		totalPage := (totalResult + resultPerPage - 1) / resultPerPage
		offset := (currentPage - 1) * resultPerPage

		productQueryStmt, err := db.Prepare("SELECT * FROM product" + conditions + " LIMIT ?, ?")
		if err != nil {
			fmt.Println("Failed to prepare product query statement", err.Error())
			http.Error(w, "Failed to prepare product query statement", http.StatusInternalServerError)
			return
		}
		defer productQueryStmt.Close()

		args = append(args, offset, resultPerPage)

		rows, err := productQueryStmt.Query(args...)
		if err != nil {
			http.Error(w, "Failed to fetch products", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var products []Product
		for rows.Next() {
			var product Product
			err := rows.Scan(&product.ProductID, &product.ProductName, &product.ProductCategory, &product.ProductPrice, &product.ProductQuantity, &product.ProductState, &product.ProductDescription, &product.SellerID, &product.ProductImageUrl)
			if err != nil {
				fmt.Println("Failed to scan product data", err.Error())
				http.Error(w, "Failed to scan product data", http.StatusInternalServerError)
				return
			}
			products = append(products, product)
		}

		// Cache the result
		cachedResponse := CachedResponse{
			Products:   products,
			TotalPages: totalPage,
		}

		cacheData, err := json.Marshal(cachedResponse)
		if err != nil {
			log.Printf("Failed to marshal products for caching: %v\n", err)
		} else {
			err := rdb.Set(ctx, cacheKey, cacheData, 3*time.Minute).Err()
			if err != nil {
				log.Printf("Failed to set cache: %v\n", err)
			}
		}

		// Send response
		sendJSONResponse(w, http.StatusOK, Response{
			Status:     "success",
			Data:       products,
			TotalPages: totalPage,
		})
		fmt.Println("data send")
	} else if err != nil {
		log.Printf("Failed to retrieve cache: %v\n", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	} else {
		// Cache hit, use cached data
		var cachedResponse CachedResponse
		err := json.Unmarshal([]byte(cachedProducts), &cachedResponse)
		if err != nil {
			log.Printf("Failed to unmarshal cached products: %v\n", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		sendJSONResponse(w, http.StatusOK, Response{
			Status:     "success",
			Data:       cachedResponse.Products,
			TotalPages: cachedResponse.TotalPages,
		})
		log.Printf("Send data from redis")
	}
}

func loadImage(w http.ResponseWriter, r *http.Request) {
	filename := filepath.Base(r.URL.Path)
	fmt.Println(filename)
	file, err := os.Open(filepath.Join("uploads", filename))
	if err != nil {
		log.Printf("Error opening image file: %v\n", err)
		http.Error(w, "Image not found", http.StatusNotFound)
		return
	}
	defer file.Close()

	// Get file extension
	ext := filepath.Ext(filename)

	// Set content type based on file extension
	contentType := "image/" + ext[1:] // Remove the dot from the extension

	// Set the content type header
	w.Header().Set("Content-Type", contentType)

	// Serve the file
	http.ServeContent(w, r, filename, time.Now(), file)
}

func loadImage2(w http.ResponseWriter, r *http.Request) {
	// Extract image ID from URL path
	imagePath := strings.TrimPrefix(r.URL.Path, "/api/tanam/loadimage/")
	// Assuming imagePath contains the path to your image file
	imageFile, err := os.Open(imagePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Image not found: %s", err.Error()), http.StatusNotFound)
		return
	}
	defer imageFile.Close()

	// Set content type based on file extension
	contentType := mime.TypeByExtension(filepath.Ext(imagePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)

	// Serve the file content
	http.ServeFile(w, r, imagePath)
}
