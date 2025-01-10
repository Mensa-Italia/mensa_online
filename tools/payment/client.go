package payment

import (
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/client"
	"mensadb/tools/env"
)

func GetClient() *client.API {
	sc := &client.API{}
	sc.Init(env.GetStripeSecret(), nil)
	return sc
}

func NewCustomer(userId, name, email string) (*stripe.Customer, error) {
	customerParams := &stripe.CustomerParams{
		Email: &email,
		Name:  &name,
		Metadata: map[string]string{
			"user_id":    userId,
			"created_by": "mensa online",
		},
	}
	return GetClient().Customers.New(customerParams)
}
