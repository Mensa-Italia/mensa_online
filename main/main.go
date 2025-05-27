package main

import (
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"log"
	"mensadb/importers"
	"mensadb/main/api"
	"mensadb/main/hooks"
	_ "mensadb/migrations"
	"mensadb/printful"
	"mensadb/tolgee"
	"mensadb/tools/dbtools"
	"mensadb/tools/env"
	"os"
	"strings"
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
		e.Router.GET("/force-stamp-gen/:id", hooks.ForceStampGen)
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
