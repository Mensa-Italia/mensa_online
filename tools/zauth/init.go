package zauth

import (
	"context"
	"github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/user/v2"
	"log"
	"log/slog"
	"os"

	"github.com/zitadel/oidc/v3/pkg/oidc"

	"github.com/zitadel/zitadel-go/v3/pkg/client"
	"github.com/zitadel/zitadel-go/v3/pkg/zitadel"
)

var API *client.Client
var ctx = context.Background()

func init() {

	domain := "auth.mensa.it"
	clientID := "internal"
	clientSecret := "oL8q2pKxpUUGVzUVDSMyMyJ6IKKJ4nWdVxfjmeOVAJkivkk30kbr8WfevIuvbgEN"

	authOption := client.PasswordAuthentication(
		clientID,
		clientSecret,
		oidc.BearerToken,
		client.ScopeZitadelAPI(),
	)

	var err error
	API, err = client.New(ctx, zitadel.New(domain),
		client.WithAuth(authOption),
	)
	if err != nil {
		slog.Error("could not create api client", "error", err)
		os.Exit(1)
	}
}

func CreateUser(username string, password string) {
	createUser, err := API.UserServiceV2().CreateUser(ctx, &user.CreateUserRequest{
		OrganizationId: "o-345084298942027074",
		Username:       &username,
	})
	if err != nil {
		log.Println("Error creating user:", err)
		return
	}

	_, err = API.UserServiceV2().SetPassword(ctx, &user.SetPasswordRequest{
		UserId: createUser.Id,
		NewPassword: &user.Password{
			Password:       password,
			ChangeRequired: false,
		},
	})
	if err != nil {
		log.Println("Error setting password:", err)
		return
	}

	_, err = API.UserServiceV2().SetEmail(ctx, &user.SetEmailRequest{
		UserId: createUser.Id,
		Email:  username,
	})
	if err != nil {
		log.Println("Error setting email:", err)
		return
	}
}
