package zauth

import (
	"context"
	"log"
	"log/slog"
	"mensadb/tools/env"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/montevive/go-name-detector/pkg/detector"
	"github.com/montevive/go-name-detector/pkg/types"
	"github.com/zitadel/zitadel-go/v3/pkg/client"
	v21 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/object/v2"
	"github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/user/v2"
	"github.com/zitadel/zitadel-go/v3/pkg/zitadel"
)

var apiClient *client.Client
var ctx = client.BearerTokenCtx(context.Background(), env.GetZitadelPAT())

func init() {

	domain := env.GetZitadelHost()

	authOption := client.PAT(env.GetZitadelPAT())

	var err error
	apiClient, err = client.New(ctx, zitadel.New(domain), client.WithAuth(authOption))
	if err != nil {
		slog.Error("could not create api client", "error", err)
		return
	}
}

func IdentifyName(name string) types.PIIResult {

	// Create detector with embedded data - works out of the box!
	d, err := detector.NewDefault()
	if err != nil {
		log.Fatal(err)
	}

	// Detect PII
	words := strings.Split(name, " ")
	result := d.DetectPII(words)

	return result
}

func UserExists(aliasMail string) (string, bool) {
	users, err := apiClient.UserServiceV2().ListUsers(ctx, &user.ListUsersRequest{
		Queries: []*user.SearchQuery{
			&user.SearchQuery{
				Query: &user.SearchQuery_UserNameQuery{
					UserNameQuery: &user.UserNameQuery{
						UserName: aliasMail,
						Method:   v21.TextQueryMethod_TEXT_QUERY_METHOD_EQUALS_IGNORE_CASE,
					},
				},
			},
		}})
	if err != nil {
		slog.Error("failed to list users", "error", err)
	}
	if len(users.Result) > 0 {
		return users.Result[0].UserId, true
	}
	return "", false
}

func CreateUser(name string, aliasMail string, originalMail string, rawMetadata map[string]string) {
	if strings.TrimSpace(aliasMail) == "" {
		return
	}
	userId, exists := UserExists(aliasMail)
	if exists {
		UpdateUser(userId, name, aliasMail, originalMail, rawMetadata)
		return
	}

	var metadata []*user.Metadata
	for key, value := range rawMetadata {
		metadata = append(metadata, &user.Metadata{
			Key:   key,
			Value: []byte(value),
		})
	}

	nameDeepInfos := IdentifyName(name)
	gender := user.Gender_GENDER_MALE
	if IdentifyName(name).Details.Gender == "Female" {
		gender = user.Gender_GENDER_FEMALE
	} else if IdentifyName(name).Details.Gender == "Male" {
		gender = user.Gender_GENDER_MALE
	}

	_, err := apiClient.UserServiceV2().CreateUser(ctx, &user.CreateUserRequest{
		OrganizationId: env.GetZitadelOrganizationID(),
		Username:       aws.String(aliasMail),
		UserType: &user.CreateUserRequest_Human_{
			Human: &user.CreateUserRequest_Human{
				Profile: &user.SetHumanProfile{
					GivenName:         strings.Join(nameDeepInfos.Details.FirstNames, " "),
					FamilyName:        strings.Join(nameDeepInfos.Details.Surnames, " "),
					NickName:          aws.String(strings.Split(aliasMail, "@")[0]),
					DisplayName:       aws.String(name),
					Gender:            &gender,
					PreferredLanguage: aws.String("it"),
				},
				Email: &user.SetHumanEmail{
					Email: originalMail,
					Verification: &user.SetHumanEmail_IsVerified{
						IsVerified: true,
					},
				},
				Metadata: metadata,
			},
		},
	})
	if err != nil {
		slog.Error("failed to create user", "error", err)
		return
	}
}

func UpdateUser(userID string, name string, aliasMail string, originalMail string, rawMetadata map[string]string) {
	if apiClient == nil {
		slog.Error("api client not initialized")
		return
	}
	if strings.TrimSpace(userID) == "" {
		slog.Error("missing userID")
		return
	}

	// --- derive profile fields from name
	nameDeepInfos := IdentifyName(name)

	// Gender: be conservative (unspecified if detector is unsure)
	gender := user.Gender_GENDER_UNSPECIFIED
	switch nameDeepInfos.Details.Gender {
	case "Female":
		gender = user.Gender_GENDER_FEMALE
	case "Male":
		gender = user.Gender_GENDER_MALE
	}

	given := strings.Join(nameDeepInfos.Details.FirstNames, " ")
	family := strings.Join(nameDeepInfos.Details.Surnames, " ")
	nick := strings.Split(aliasMail, "@")[0]
	display := name
	lang := "it"

	// --- update user (username + human patch)
	_, err := apiClient.UserServiceV2().UpdateUser(ctx, &user.UpdateUserRequest{
		UserId:   userID,
		Username: aws.String(aliasMail),
		UserType: &user.UpdateUserRequest_Human_{
			Human: &user.UpdateUserRequest_Human{
				Profile: &user.UpdateUserRequest_Human_Profile{
					GivenName:         &given,
					FamilyName:        &family,
					NickName:          &nick,
					DisplayName:       &display,
					PreferredLanguage: &lang,
					Gender:            &gender,
				},
				Email: &user.SetHumanEmail{
					Email: originalMail,
					Verification: &user.SetHumanEmail_IsVerified{
						IsVerified: true,
					},
				},
			},
		},
	})
	if err != nil {
		slog.Error("failed to update user", "userID", userID, "error", err)
		return
	}

	// --- upsert metadata (only if provided)
	if len(rawMetadata) > 0 {
		metadata := make([]*user.Metadata, 0, len(rawMetadata))
		for key, value := range rawMetadata {
			metadata = append(metadata, &user.Metadata{
				Key:   key,
				Value: []byte(value),
			})
		}

		_, err = apiClient.UserServiceV2().SetUserMetadata(ctx, &user.SetUserMetadataRequest{
			UserId:   userID,
			Metadata: metadata,
		})
		if err != nil {
			slog.Error("failed to set user metadata", "userID", userID, "error", err)
			return
		}
	}

	slog.Info("user updated", "userID", userID, "username", aliasMail)
}
