package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
)

type CartRequest struct {
	ProductID    string `json:"product_id"`
	CartQuantity string `json:"cart_quantity"`
	CartPrice    string `json:"cart_price"`
	BuyerID      string `json:"buyer_id"`
	SellerID     string `json:"seller_id"`
}

func addCart(w http.ResponseWriter, r *http.Request) {
	var req CartRequest
	err := json.NewDecoder(r.Body).Decode(&req)

	if err != nil {
		log.Printf("JSON decoding error: %v\n", err)
		sendJSONResponse(w, http.StatusBadRequest, Response{Status: "failed", Data: "Invalid JSON"})
		return
	}

	product_id := req.ProductID
	cart_quantity := req.CartQuantity
	cart_price := req.CartPrice
	buyer_id := req.BuyerID
	seller_id := req.SellerID
	price, err := strconv.ParseFloat(cart_price, 64)
	if err != nil {
		http.Error(w, "Invalid product price", http.StatusBadRequest)
		return
	}
	quantity, err := strconv.Atoi(cart_quantity)
	if err != nil {
		http.Error(w, "Invalid product quantity", http.StatusBadRequest)
		return
	}
	fmt.Println(product_id)
	fmt.Println(cart_quantity)
	fmt.Println(cart_price)
	fmt.Println(buyer_id)
	fmt.Println(seller_id)
	if product_id == "" || cart_quantity == "" || cart_price == "" || buyer_id == "" || seller_id == "" {
		fmt.Println("Form empty error")
		sendJSONResponse(w, http.StatusBadRequest, Response{Status: "failed", Data: nil})
		return
	}
	db, err := dbConnect(w)
	if err != nil {
		fmt.Println("dbconnect error:", err.Error())
		return
	}
	defer db.Close()

	//check if user already add this product to his cart before or not
	stmtCount, err := db.Prepare("SELECT COUNT(*) FROM cart WHERE buyer_id = ? AND product_id = ?")
	if err != nil {
		fmt.Println("SQL Prepare error0:", err.Error())
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "failed", Data: err})
		return
	}
	defer stmtCount.Close()

	var count int
	err = stmtCount.QueryRow(buyer_id, product_id).Scan(&count)
	if err != nil {
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "failed", Data: nil})
		return
	}
	if count > 0 {
		stmt, err := db.Prepare("UPDATE cart SET cart_quantity = ?, cart_price = ? WHERE buyer_id = ? AND product_id = ?")
		if err != nil {
			fmt.Println("SQL Prepare error1:", err.Error())
			sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "failed", Data: err})
			return
		}
		defer stmtCount.Close()
		_, err = stmt.Exec(quantity, price, buyer_id, product_id, seller_id)
		if err != nil {
			log.Printf("SQL execution error2: %v\n", err)
			sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "failed", Data: err.Error()})
			return
		}

	} else {
		stmt, err := db.Prepare("INSERT INTO cart (cart_quantity, cart_price, buyer_id, product_id, seller_id) VALUES(?, ?, ?, ?, ?)")
		if err != nil {
			fmt.Println("SQL Prepare error3:", err.Error())
			sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "failed", Data: err})
			return
		}
		defer stmtCount.Close()
		_, err = stmt.Exec(quantity, price, buyer_id, product_id, seller_id)
		if err != nil {
			log.Printf("SQL execution error: %v\n", err)
			sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "failed", Data: err.Error()})
			return
		}

	}

	sendJSONResponse(w, http.StatusOK, Response{Status: "success", Data: "Cart updated successfully"})

}
