package zauth

import (
	"context"
	"log"
	"log/slog"
	"mensadb/tools/env"
	"mensadb/tools/nameorder"
	"regexp"
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

var nonLetters = regexp.MustCompile(`[^\p{L}\p{M}\s'-]+`) // lascia lettere unicode + spazi + ' -

func tokenizeFullName(s string) []string {
	s = strings.TrimSpace(s)
	s = nonLetters.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return nil
	}
	return strings.Split(s, " ")
}

func IdentifyName(name string, email string) types.PIIResult {
	d, err := detector.NewDefault()
	if err != nil {
		log.Fatal(err)
	}
	ordered := nameorder.OrderTokensByEmailLocalPart(name, email)
	log.Println(ordered)

	return d.DetectPII(ordered)
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

	// Filtro metadati: include solo quelli con valore non vuoto
	var metadata []*user.Metadata
	for key, value := range rawMetadata {
		if len(value) > 0 {
			metadata = append(metadata, &user.Metadata{
				Key:   key,
				Value: []byte(value),
			})
		}
	}

	nameDeepInfos := IdentifyName(name, aliasMail)
	gender := user.Gender_GENDER_MALE
	if nameDeepInfos.Details.Gender == "Female" {
		gender = user.Gender_GENDER_FEMALE
	} else if nameDeepInfos.Details.Gender == "Male" {
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
					DisplayName:       aws.String(strings.Join(nameDeepInfos.Details.FirstNames, " ") + " " + strings.Join(nameDeepInfos.Details.Surnames, " ")),
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
	nameDeepInfos := IdentifyName(name, aliasMail)

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
	display := given + " " + family
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

	// --- upsert metadata (only if provided and value is not empty)
	if len(rawMetadata) > 0 {
		metadata := make([]*user.Metadata, 0, len(rawMetadata))
		for key, value := range rawMetadata {
			if len(value) > 0 {
				metadata = append(metadata, &user.Metadata{
					Key:   key,
					Value: []byte(value),
				})
			}
		}

		// Procedi solo se dopo il filtraggio abbiamo effettivamente dei metadati da inviare
		if len(metadata) > 0 {
			_, err = apiClient.UserServiceV2().SetUserMetadata(ctx, &user.SetUserMetadataRequest{
				UserId:   userID,
				Metadata: metadata,
			})
			if err != nil {
				slog.Error("failed to set user metadata", "userID", userID, "error", err)
				return
			}
		}
	}

	slog.Info("user updated", "userID", userID, "username", aliasMail)
}
