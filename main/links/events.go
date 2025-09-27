package links

import (
	"bytes"
	"github.com/pocketbase/pocketbase/core"
	"html/template"
)

const htmlTemplate = `<!DOCTYPE html>
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
      max-width: 400px;
    }

    h1 {
      font-size: 24px;
      margin-bottom: 16px;
    }

    p {
      font-size: 16px;
      margin-bottom: 24px;
    }

    .btn {
      display: inline-block;
      margin: 10px;
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
  </style>
</head>
<body>
  <div class="container">
    <h1>Per continuare, scarica l'app ufficiale di Mensa Italia</h1>
    <p>Questa pagina è disponibile all'interno dell'app Mensa Italia.</p>
    <a href="https://apps.apple.com/app/id1524200080" class="btn">App Store</a>
    <a href="https://play.google.com/store/apps/details?id=it.mensa.app" class="btn">Google Play</a>
    <a href="https://web.svc.mensa.it/events/{{.Id}}" class="btn">Vedi Online</a>
  </div>
  <script>
  function isMobile() {
    // Browser moderni con User-Agent Client Hints
    if (navigator.userAgentData && navigator.userAgentData.mobile !== undefined) {
      return navigator.userAgentData.mobile;
    }
    // Fallback per compatibilità
    return /Android|iPhone|iPad|iPod|IEMobile|Opera Mini/i.test(navigator.userAgent);
  }

  if (!isMobile()) {
    setTimeout(function() {
      window.location.href = "https://web.svc.mensa.it/events/{{.Id}}";
    }, 500);
  }
</script>
</body>
</html>`

const htmlErrorTemplate = `<!DOCTYPE html>
<html lang="it">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  
  <!-- Meta per social -->
  <meta property="og:title" content="Evento non trovato - Mensa Italia">
  <meta property="og:description" content="L'evento che stai cercando non è disponibile o è stato rimosso.">
  <meta property="og:image" content="https://svc.mensa.it/error-cover.jpg">
  <meta property="og:type" content="website">
  <meta property="og:url" content="https://svc.mensa.it/errore-evento">

  <meta name="twitter:card" content="summary_large_image">
  <meta name="twitter:title" content="Evento non trovato - Mensa Italia">
  <meta name="twitter:description" content="L'evento che stai cercando non è disponibile o è stato rimosso.">
  <meta name="twitter:image" content="https://svc.mensa.it/error-cover.jpg">

  <title>Evento non trovato - Mensa Italia</title>

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
      max-width: 400px;
    }

    h1 {
      font-size: 24px;
      margin-bottom: 16px;
      color: #d32f2f;
    }

    p {
      font-size: 16px;
      margin-bottom: 24px;
    }

    .btn {
      display: inline-block;
      margin: 10px;
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
  </style>
</head>
<body>
  <div class="container">
    <h1>Evento non trovato</h1>
    <p>L'evento che stai cercando potrebbe essere scaduto, rimosso o non esistere.</p>
    <a href="https://mensa.it" class="btn">Torna alla home</a>
  </div>
</body>
</html>`

type EventTemplateData struct {
	Id          string
	Title       string
	Description string
	Image       string
	URL         string
}

func LinksEvents(e *core.RequestEvent) error {
	idEvent := e.Request.PathValue("id")
	app := e.App

	collection, _ := app.FindCollectionByNameOrId("events")
	if collection == nil {
		return e.String(404, "Collection 'events' not found")
	}

	record, err := app.FindRecordById(collection.Id, idEvent)
	if err != nil || record == nil {
		return e.HTML(404, htmlErrorTemplate)
	}

	imageKey := record.BaseFilesPath() + "/" + record.GetString("image")

	data := EventTemplateData{
		Id:          idEvent,
		Title:       record.GetString("name"),
		Description: record.GetString("description"),
		Image:       "https://svc.mensa.it/api/files/" + imageKey,
		URL:         "https://svc.mensa.it/links/event/" + idEvent,
	}

	tmpl, err := template.New("event").Parse(htmlTemplate)
	if err != nil {
		return e.String(500, "Template parsing error")
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return e.String(500, "Template execution error")
	}

	return e.HTML(200, buf.String())
}
