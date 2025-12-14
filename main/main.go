package main

import (
	"context"
	"log"
	"mensadb/importers"
	"mensadb/main/api"
	"mensadb/main/hooks"
	"mensadb/main/links"
	"mensadb/main/utilities"
	_ "mensadb/migrations"
	"mensadb/printful"
	"mensadb/tolgee"
	"mensadb/tools/dbtools"
	"mensadb/tools/env"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
)

func main() {
	app := pocketbase.New()

	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		tolgee.Load(env.GetTolgeeKey())
		printful.Setup(env.GetPrintfulKey())
		printful.SetupWebhook(env.GetPrintfulWebhookURL())
		go importers.GetFullMailList()
		return e.Next()
	})

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		api.Load(e.Router.Group("/api"))
		e.Router.GET("/ical/{hash}", RetrieveICAL)
		e.Router.GET("/static/{path...}", apis.Static(os.DirFS("./pb_public"), false))
		e.Router.GET("/force-stamp-gen/{id}", hooks.ForceStampGen)
		e.Router.GET("/.well-known/apple-app-site-association", utilities.AASAWellKnown)
		e.Router.GET("/.well-known/assetlinks.json", utilities.AssetLinksWellKnown)
		e.Router.GET("/links/event/{id}", links.LinksEvents)
		return e.Next()
	})

	app.OnFileDownloadRequest().BindFunc(func(e *core.FileDownloadRequestEvent) error {
		s3settings := app.Settings().S3
		if s3settings.Enabled {
			s3client, err := NewS3(s3settings.Bucket, s3settings.Region, s3settings.Endpoint, s3settings.AccessKey, s3settings.Secret, s3settings.ForcePathStyle)
			if err != nil {
				app.Logger().Error("create s3 client", err)
				return nil
			}
			presignClient := s3.NewPresignClient(s3client)
			presignedUrl, err := presignClient.PresignGetObject(context.Background(),
				&s3.GetObjectInput{
					Bucket: aws.String(s3settings.Bucket),
					Key:    aws.String(e.ServedPath),
				},
				s3.WithPresignExpires(time.Hour))
			if err != nil {
				app.Logger().Error("create s3 presigned url", err)
				return nil
			}

			return e.Redirect(http.StatusTemporaryRedirect, presignedUrl.URL)
		}

		return e.Next()
	})

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		dbtools.RemoteUpdateAddons(e.App)
		dbtools.RemoteRetrieveDocumentsFromArea32(e.App)
		dbtools.RemoteRetrieveMembersFromArea32(e.App)
		dbtools.CronTasks(e.App)
		return e.Next()
	})

	// Hooks to table events
	hooks.Load(app)

	isGoRun := strings.HasPrefix(os.Args[0], os.TempDir())

	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		Automigrate: isGoRun,
		Dir:         "./migrations",
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
