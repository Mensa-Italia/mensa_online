package main

import (
	"encoding/json"
	"github.com/pocketbase/pocketbase/core"
)

func createBoutiquePaymentHandler(e *core.RequestEvent) error {
	isLogged, authUser := isLoggedIn(e)
	if !isLogged {
		return e.String(401, "Unauthorized")
	}

	var products []string
	err := json.Unmarshal([]byte(e.Request.FormValue("products")), &products)
	if err != nil {
		return e.String(400, "Invalid products")
	}

	collectionBoutique, err := app.FindCollectionByNameOrId("boutique")
	total := 0
	for _, product := range products {
		prod, err := app.FindRecordById(collectionBoutique, product)
		if err != nil {
			return e.String(404, "Product not found")
		}
		total += prod.GetInt("amount")
	}

	collectionBoutiqueOrders, err := app.FindCollectionByNameOrId("boutique_orders")
	if err != nil {
		return e.String(500, "Error finding collection")
	}
	record := core.NewRecord(collectionBoutiqueOrders)
	record.Set("user", authUser.Id)
	record.Set("boutique_products", products)

	paymentRecord, paymentIntent, err := createPayment(authUser.Id, total)
	if err != nil {
		return e.String(500, "Error creating payment intent")
	}

	record.Set("payment", paymentRecord.Id)
	record.Set("total", total)
	record.Set("status", "processing")

	err = app.Save(record)
	if err != nil {
		print(err.Error())
		return e.String(500, "Error saving order")
	}

	return e.JSON(200, paymentIntent)
}
