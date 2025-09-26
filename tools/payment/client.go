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

func ExistsCustomer(stripeCustomerId string) (bool, error) {
	_, err := GetClient().Customers.Get(stripeCustomerId, nil)
	if err != nil {
		if stripeErr, ok := err.(*stripe.Error); ok {
			if stripeErr.Code == stripe.ErrorCodeResourceMissing {
				return false, nil
			}
		}
		return false, err
	}
	return true, nil
}

func NewCustomerIfNotExists(userId, name, email string) (*stripe.Customer, error) {
	customerParams := &stripe.CustomerListParams{
		Email: &email,
	}
	i := GetClient().Customers.List(customerParams)
	for i.Next() {
		c := i.Customer()
		if c.Metadata["user_id"] == userId {
			return c, nil
		}
	}
	return NewCustomer(userId, name, email)
}
