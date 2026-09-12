package public

import (
	"bytes"
	"fmt"
	"net/http"
)

type errorPageData struct {
	PageData
	RequestID string
}

func (renderer *Renderer) WriteError(response http.ResponseWriter, request *http.Request, status int, _ string, _ string) {
	title, heading, lead := publicErrorCopy(status)
	page := PageData{
		SiteName: "Studio Legale Alessandro Callegarin", Title: title,
		Description: lead, CanonicalURL: renderer.baseURL, Path: "", Kind: "error",
		Eyebrow: "Informazione di servizio", Heading: heading, Lead: lead,
		Navigation: navigationFor(""), Robots: "noindex, nofollow",
	}
	renderer.applyPageSEO(&page, "website", renderer.images["contact-entrance"], nil)
	data := errorPageData{PageData: page, RequestID: response.Header().Get("X-Request-ID")}
	var output bytes.Buffer
	if err := renderer.errorPage.ExecuteTemplate(&output, "base", data); err != nil {
		writePublicError(response, status, "Richiesta non disponibile")
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Robots-Tag", "noindex, nofollow")
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	_, _ = response.Write(withResponseNonce(output.Bytes(), request))
}

func publicErrorCopy(status int) (title, heading, lead string) {
	switch status {
	case http.StatusNotFound:
		return "Pagina non trovata", "La pagina non è disponibile", "Controlla l’indirizzo oppure torna alla pagina iniziale."
	case http.StatusConflict:
		return "Richiesta da verificare", "La richiesta richiede una verifica", "I dati non sono stati sovrascritti. Riprova dopo aver verificato le informazioni."
	case http.StatusUnprocessableEntity:
		return "Dati da controllare", "Controlla i dati inseriti", "Alcune informazioni richiedono una correzione prima di continuare."
	case http.StatusTooManyRequests:
		return "Troppe richieste", "Attendi prima di riprovare", "Per proteggere il servizio, la richiesta può essere ripetuta tra poco."
	case http.StatusServiceUnavailable:
		return "Servizio temporaneamente non disponibile", "Il servizio non è disponibile", "Riprova più tardi oppure usa i recapiti diretti."
	default:
		return "Errore", "Si è verificato un errore", "Riprova più tardi oppure usa i recapiti diretti."
	}
}

func writePublicError(response http.ResponseWriter, status int, message string) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Robots-Tag", "noindex, nofollow")
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	_, _ = fmt.Fprintf(response, "<!doctype html><html lang=\"it\"><title>Errore</title><main><h1>%s</h1></main></html>", message)
}
