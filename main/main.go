package main

import (
	"log"
	"mensadb/importers"
	"mensadb/main/api"
	"mensadb/main/cmd/quidcmd"
	"mensadb/main/cmd/searchcmd"
	"mensadb/main/crons"
	"mensadb/main/hooks"
	"mensadb/main/api/zitadelauth"
	"mensadb/main/links"
	"mensadb/main/utilities"
	"mensadb/mcp"
	_ "mensadb/migrations"
	"mensadb/printful"
	"mensadb/tolgee"
	"mensadb/tools/cdnfiles"
	"mensadb/tools/env"
	"mensadb/tools/search"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
)

func main() {
	if err := env.MustValidate(); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	app := pocketbase.New()
	crons.CronTasks(app)

	// OnBootstrap fire per qualsiasi subcommand (serve, search-backfill,
	// migrate, ...). Tieni qui SOLO inizializzazioni leggere e idempotenti
	// che servono anche ai comandi CLI (search.Init e` indispensabile per
	// il backfill). Robe network-heavy come Printful / mail importer vanno
	// in OnServe per non bloccare le invocazioni CLI.
	app.OnBootstrap().BindFunc(func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		if err := search.Init(filepath.Join(e.App.DataDir(), "search_index")); err != nil {
			log.Fatalf("search init failed: %v", err)
		}
		return nil
	})

	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		if err := search.Shutdown(); err != nil {
			log.Printf("search shutdown: %v", err)
		}
		return e.Next()
	})

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		printful.Setup(env.GetPrintfulKey())
		printful.SetupWebhook(env.GetPrintfulWebhookURL())
		go importers.GetFullMailList()
		tolgee.Load(env.GetTolgeeKey(), e.App)
		e.Router.Bind(zitadelauth.LoadAuth())
		api.Load(e.Router.Group("/api"))
		e.Router.GET("/ical/{hash}", RetrieveICAL)
		e.Router.GET("/static/{path...}", apis.Static(os.DirFS("./pb_public"), false))
		e.Router.GET("/force-stamp-gen/{id}", hooks.ForceStampGen)
		e.Router.GET("/.well-known/apple-app-site-association", utilities.AASAWellKnown)
		e.Router.GET("/.well-known/assetlinks.json", utilities.AssetLinksWellKnown)
		e.Router.GET("/links/event/{id}", links.LinksEvents)
		e.Router.GET("/links/stamp/{id}", links.LinksStamps)

		e.Router.GET("/.well-known/oauth-protected-resource", mcp.WellKnownProtectedResourceHandler(e.App))
		e.Router.GET("/.well-known/oauth-authorization-server", mcp.WellKnownAuthServerHandler())
		e.Router.GET("/authorize", mcp.AuthorizeRedirectHandler())

		// Salva l'addr del server PB cosi` i tool MCP che fanno proxy verso
		// /api/collections/... via loopback (per rispettare le list/view rule)
		// sanno dove andare. Deve avvenire PRIMA di mcp.Init.
		if e.Server != nil {
			mcp.SetServerAddr(e.Server.Addr)
		}
		mcpHandler := mcp.Init(e.App)
		e.Router.Any("/mcp", func(re *core.RequestEvent) error {
			mcpHandler.ServeHTTP(re.Response, re.Request)
			return nil
		})

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

	app.RootCmd.AddCommand(searchcmd.New(app))
	app.RootCmd.AddCommand(quidcmd.New(app))

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
