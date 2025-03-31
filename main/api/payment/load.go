package payment

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"mensadb/main/api/payment/receipt"
)

func Load(e *router.RouterGroup[*core.RequestEvent]) {
	receipt.Load(e.Group("/receipt"))

	e.POST("/method", PaymentMethodCreateHandler)
	e.GET("/method", GetPaymentMethodsHandler)
	e.POST("/default", setDefaultPaymentMethod)
	e.GET("/customer", getCustomerHandler)
	e.POST("/donate", donateHandler)
	e.POST("/webhook", webhookStripe)
	e.GET("/{id}", getPaymentIntentHandler)
	e.POST("/boutique", createBoutiquePaymentHandler)
}
