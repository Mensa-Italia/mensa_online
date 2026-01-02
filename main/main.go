package main

import (
	"log"
	"mensadb/importers"
	"mensadb/main/api"
	"mensadb/main/hooks"
	"mensadb/main/links"
	"mensadb/main/utilities"
	_ "mensadb/migrations"
	"mensadb/printful"
	"mensadb/tolgee"
	"mensadb/tools/cdnfiles"
	"mensadb/tools/dbtools"
	"mensadb/tools/env"
	"net/http"
	"os"
	"strings"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
)

func main() {
	app := pocketbase.New()
	dbtools.CronTasks(app)

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
		e.Router.GET("/links/stamp/{id}", links.LinksStamps)
		return e.Next()
	})

	app.OnFileDownloadRequest().BindFunc(func(e *core.FileDownloadRequestEvent) error {
		s3settings := app.Settings().S3
		presignedUrl := cdnfiles.GetFilePresignedURL(app, s3settings.Bucket, e.ServedPath)
		if presignedUrl != "" {
			return e.Redirect(http.StatusTemporaryRedirect, presignedUrl)
		}

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
