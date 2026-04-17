package zauth

import (
	"context"
	"log"
	"log/slog"
	"mensadb/tools/env"
	"mensadb/tools/nameorder"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/montevive/go-name-detector/pkg/detector"
	"github.com/montevive/go-name-detector/pkg/types"
	"github.com/zitadel/zitadel-go/v3/pkg/client"
	"github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/filter/v2"
	v2 "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/metadata/v2"
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

func FindUserByMembershipID(membershipID string) (*user.User, bool) {
	users, err := apiClient.UserServiceV2().ListUsers(ctx, &user.ListUsersRequest{
		Queries: []*user.SearchQuery{
			{
				Query: &user.SearchQuery_AndQuery{
					AndQuery: &user.AndQuery{
						Queries: []*user.SearchQuery{
							{
								Query: &user.SearchQuery_MetadataKeyFilter{
									MetadataKeyFilter: &v2.MetadataKeyFilter{
										Key:    "membership_id",
										Method: filter.TextFilterMethod_TEXT_FILTER_METHOD_EQUALS_IGNORE_CASE,
									},
								},
							},
							{
								Query: &user.SearchQuery_MetadataValueFilter{
									MetadataValueFilter: &v2.MetadataValueFilter{
										Value:  []byte(membershipID),
										Method: filter.ByteFilterMethod_BYTE_FILTER_METHOD_EQUALS,
									},
								},
							},
						},
					},
				},
			},
		}})
	if err != nil {
		slog.Error("failed to list users", "error", err)
	}
	if len(users.Result) > 0 {
		return users.Result[0], true
	}
	return nil, false
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

	// Filtro metadati: include solo quelli con valore non vuot
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
	switch nameDeepInfos.Details.Gender {
	case "Female":
		gender = user.Gender_GENDER_FEMALE
	case "Male":
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

func SetUserPassword(membershipID string, password string) {
	if apiClient == nil {
		slog.Error("api client not initialized")
		return
	}
	userFound, found := FindUserByMembershipID(membershipID)
	if !found {
		slog.Error("user not found", "membershipID", membershipID)
		return
	}
	_, err := apiClient.UserServiceV2().UpdateUser(ctx, &user.UpdateUserRequest{
		UserId: userFound.UserId,
		UserType: &user.UpdateUserRequest_Human_{
			Human: &user.UpdateUserRequest_Human{
				Password: &user.SetPassword{
					PasswordType: &user.SetPassword_Password{
						Password: &user.Password{
							Password:       password,
							ChangeRequired: false,
						},
					},
				},
			},
		},
	})
	if err != nil {
		slog.Error("failed to set user password", "userID", userFound.UserId, "error", err)
		return
	}

	_, _ = apiClient.UserServiceV2().SetUserMetadata(ctx, &user.SetUserMetadataRequest{
		UserId: userFound.UserId,
		Metadata: []*user.Metadata{
			{
				Key:   "area32_password_set",
				Value: []byte("true"),
			},
		},
	})
	slog.Info("user password set", "userID", userFound.UserId)
}
