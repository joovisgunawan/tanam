package main

import (
	"database/sql"
	"net/http"

	_ "github.com/go-sql-driver/mysql"
)

func dbConnect(w http.ResponseWriter) (*sql.DB, error) {
	db, err := sql.Open("mysql", "tanam:t4nAm_mariadb@tcp(tanam.software:3306)/tanam")
	if err != nil {
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "database connection failed", Data: err.Error()})
		return nil, err
	}

	// Check if the connection is actually established
	err = db.Ping()
	if err != nil {
		db.Close()
		sendJSONResponse(w, http.StatusInternalServerError, Response{Status: "database connection ping failed", Data: err.Error()})
		return nil, err
	}

	return db, nil
}
