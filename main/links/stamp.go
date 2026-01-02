package links

import (
	"bytes"
	"html/template"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// NOTE:
// - This link is meant to be opened inside the Mensa Italia app.
// - If the app is not installed there is no alternative web flow.
// - We still provide App Store / Google Play buttons to install the app.

const htmlTemplateStamp = `<!DOCTYPE html>
<html lang="it">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">

  <!-- Meta per social (Telegram, WhatsApp, Facebook, Twitter) -->
  <meta property="og:title" content="{{.Title}}">
  <meta property="og:description" content="{{.Description}}">
  <meta property="og:image" content="{{.Image}}">
  <meta property="og:type" content="website">
  <meta property="og:url" content="{{.URL}}">

  <meta name="twitter:card" content="summary_large_image">
  <meta name="twitter:title" content="{{.Title}}">
  <meta name="twitter:description" content="{{.Description}}">
  <meta name="twitter:image" content="{{.Image}}">

  <title>{{.Title}}</title>

  <style>
    body {
      margin: 0;
      font-family: 'Helvetica Neue', sans-serif;
      background: #f4f4f4;
      color: #333;
      display: flex;
      align-items: center;
      justify-content: center;
      height: 100vh;
      text-align: center;
      padding: 20px;
    }

    .container {
      background: white;
      padding: 40px;
      border-radius: 12px;
      box-shadow: 0 4px 20px rgba(0,0,0,0.1);
      max-width: 440px;
      width: 100%;
    }

    h1 {
      font-size: 22px;
      margin-bottom: 14px;
    }

    p {
      font-size: 16px;
      margin-bottom: 22px;
      line-height: 1.35;
    }

    .btn {
      display: inline-block;
      margin: 8px;
      padding: 12px 20px;
      font-size: 16px;
      border-radius: 8px;
      text-decoration: none;
      color: white;
      background-color: #0072CE;
      transition: background-color 0.2s;
    }

    .btn:hover {
      background-color: #005ea5;
    }

    .btn.secondary {
      background-color: #4b5563;
    }

    .btn.secondary:hover {
      background-color: #374151;
    }

    .hint {
      font-size: 14px;
      opacity: 0.85;
      margin-top: 10px;
    }
  </style>
</head>
<body>
  <div class="container">
    <h1>Questo contenuto è disponibile solo nell'app ufficiale del Mensa Italia</h1>
    <p>
      Per aprire questo timbro devi usare l'app Mensa Italia.
      Se non l'hai installata, installala dallo store e poi riapri il link.
    </p>

    <div>
      <a href="https://apps.apple.com/app/id1524200080" class="btn secondary">App Store</a>
      <a href="https://play.google.com/store/apps/details?id=it.mensa.app" class="btn secondary">Google Play</a>
    </div>
  </div>

</body>
</html>`

const htmlErrorTemplateStamp = `<!DOCTYPE html>
<html lang="it">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">

  <!-- Meta per social -->
  <meta property="og:title" content="Timbro non trovato - Mensa Italia">
  <meta property="og:description" content="Il timbro che stai cercando non è disponibile o è stato rimosso.">
  <meta property="og:image" content="https://svc.mensa.it/error-cover.jpg">
  <meta property="og:type" content="website">
  <meta property="og:url" content="https://svc.mensa.it/errore-timbro">

  <meta name="twitter:card" content="summary_large_image">
  <meta name="twitter:title" content="Timbro non trovato - Mensa Italia">
  <meta name="twitter:description" content="Il timbro che stai cercando non è disponibile o è stato rimosso.">
  <meta name="twitter:image" content="https://svc.mensa.it/error-cover.jpg">

  <title>Timbro non trovato - Mensa Italia</title>

  <style>
    body {
      margin: 0;
      font-family: 'Helvetica Neue', sans-serif;
      background: #f4f4f4;
      color: #333;
      display: flex;
      align-items: center;
      justify-content: center;
      height: 100vh;
      text-align: center;
      padding: 20px;
    }

    .container {
      background: white;
      padding: 40px;
      border-radius: 12px;
      box-shadow: 0 4px 20px rgba(0,0,0,0.1);
      max-width: 440px;
      width: 100%;
    }

    h1 {
      font-size: 22px;
      margin-bottom: 14px;
      color: #d32f2f;
    }

    p {
      font-size: 16px;
      margin-bottom: 22px;
      line-height: 1.35;
    }

    .btn {
      display: inline-block;
      margin: 8px;
      padding: 12px 20px;
      font-size: 16px;
      border-radius: 8px;
      text-decoration: none;
      color: white;
      background-color: #0072CE;
      transition: background-color 0.2s;
    }

    .btn:hover {
      background-color: #005ea5;
    }

    .btn.secondary {
      background-color: #4b5563;
    }

    .btn.secondary:hover {
      background-color: #374151;
    }
  </style>
</head>
<body>
  <div class="container">
    <h1>Timbro non trovato</h1>
    <p>Il timbro che stai cercando potrebbe essere scaduto, rimosso o non esistere.</p>
    <a href="https://apps.apple.com/app/id1524200080" class="btn secondary">App Store</a>
    <a href="https://play.google.com/store/apps/details?id=it.mensa.app" class="btn secondary">Google Play</a>
  </div>
</body>
</html>`

type StampTemplateData struct {
	Id          string
	Title       string
	Description string
	Image       string
	URL         string
}

// LinksStamps serves the "stamp" share link.
// It returns a social-preview-friendly HTML page that attempts to open the Mensa Italia app.
// There is intentionally no web fallback.
func LinksStamps(e *core.RequestEvent) error {
	idStamp := e.Request.PathValue("id")
	idStamp = strings.Split(idStamp, ":::")[0]
	app := e.App

	collection, _ := app.FindCollectionByNameOrId("stamp")
	if collection == nil {
		return e.String(404, "Collection 'stamps' not found")
	}

	record, err := app.FindRecordById(collection.Id, idStamp)
	if err != nil || record == nil {
		return e.HTML(404, htmlErrorTemplateStamp)
	}

	imageKey := record.BaseFilesPath() + "/" + record.GetString("image")

	data := StampTemplateData{
		Id:          idStamp,
		Title:       record.GetString("name"),
		Description: record.GetString("description"),
		Image:       "https://svc.mensa.it/api/files/" + imageKey,
		URL:         "https://svc.mensa.it/links/stamp/" + idStamp,
	}

	tmpl, err := template.New("stamp").Parse(htmlTemplateStamp)
	if err != nil {
		return e.String(500, "Template parsing error")
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return e.String(500, "Template execution error")
	}

	return e.HTML(200, buf.String())
}
