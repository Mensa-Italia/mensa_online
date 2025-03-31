package main

import (
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
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
		return e.Next()
	})

	// Hooks to table events
	hooks.Load(app)

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
