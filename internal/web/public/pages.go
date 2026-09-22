package public

import "html/template"

type PageKind string

const (
	pageKindHome          PageKind = "home"
	pageKindProfile       PageKind = "profile"
	pageKindApproach      PageKind = "approach"
	pageKindAreas         PageKind = "areas"
	pageKindArea          PageKind = "area"
	pageKindArticles      PageKind = "articles"
	pageKindArticle       PageKind = "article"
	pageKindContact       PageKind = "contact"
	pageKindPrivacyPolicy PageKind = "privacy-policy"
)

type PageData struct {
	SiteName          string
	Title             string
	Description       string
	CanonicalURL      string
	Path              string
	Kind              PageKind
	Eyebrow           string
	Heading           string
	Lead              string
	Navigation        []NavigationItem
	HeroImage         EditorialImage
	ApproachImage     EditorialImage
	ArticleImage      EditorialImage
	ContactImage      EditorialImage
	Sections          []ContentSection
	Areas             []PracticeArea
	HighlightedAreas  []PracticeArea
	Contact           StudioContact
	Robots            string
	OpenGraphType     string
	OpenGraphURL      string
	OpenGraphImageURL string
	StructuredData    template.JS
	CSPNonce          string
	Articles          []ArticleCard
}

type StudioContact struct {
	PhoneDisplay string
	PhoneHref    string
	Email        string
	EmailHref    string
	PEC          string
	PECHref      string
	Address      string
	Hours        string
}

var confirmedStudioContact = StudioContact{
	PhoneDisplay: "0331 792529",
	PhoneHref:    "tel:+390331792529",
	Email:        "callegarinale@gmail.com",
	EmailHref:    "mailto:callegarinale@gmail.com",
	PEC:          "alessandro.callegarin@busto.pecavvocati.it",
	PECHref:      "mailto:alessandro.callegarin@busto.pecavvocati.it",
	Address:      "Via Borghi 8, Gallarate (VA)",
	Hours:        "Dal lunedì al venerdì, 09:00–12:30 e 15:00–19:00",
}

type NavigationItem struct {
	Label   string
	Href    string
	Current bool
}

type EditorialImage struct {
	ID            string
	Alt           string
	LandscapeAVIF string
	LandscapeWebP string
	CardAVIF      string
	CardWebP      string
}

type ContentSection struct {
	Heading    string
	Paragraphs []string
	Items      []string
}

type PracticeArea struct {
	Title       string
	Href        string
	Description string
	Image       EditorialImage
}

func pageCatalog(images map[string]EditorialImage) map[string]PageData {
	areas := []PracticeArea{
		{Title: "Famiglia e persone", Href: "/aree-di-attivita/famiglia-e-persone", Description: "Separazione, divorzio, amministrazione di sostegno e procedimenti davanti al tribunale per i minorenni.", Image: images["family-objects"]},
		{Title: "Successioni e donazioni", Href: "/aree-di-attivita/successioni-e-donazioni", Description: "Assistenza nelle successioni, nelle donazioni e nella predisposizione delle dichiarazioni di successione.", Image: images["succession-seal"]},
		{Title: "Obbligazioni e contratti", Href: "/aree-di-attivita/obbligazioni-e-contratti", Description: "Locazioni, compravendite, appalti e rapporti obbligatori letti con chiarezza e precisione.", Image: images["contracts-pen"]},
		{Title: "Recupero crediti", Href: "/aree-di-attivita/recupero-crediti", Description: "Valutazione e gestione del credito, dalla fase stragiudiziale alle azioni consentite.", Image: images["debt-ledger"]},
		{Title: "Risarcimento danni", Href: "/aree-di-attivita/risarcimento-danni", Description: "Danni da circolazione stradale, responsabilità medica e altre ipotesi risarcitorie.", Image: images["damages-road"]},
		{Title: "Diritti reali", Href: "/aree-di-attivita/diritti-reali", Description: "Proprietà, usufrutto, servitù, pegno e ipoteca nei rapporti tra persone e patrimoni.", Image: images["property-key"]},
		{Title: "Diritto penale", Href: "/aree-di-attivita/diritto-penale", Description: "Reati contro la persona, la famiglia e il patrimonio, oltre ai reati stradali.", Image: images["criminal-threshold"]},
		{Title: "Diritto tributario", Href: "/aree-di-attivita/diritto-tributario", Description: "Assistenza nel contenzioso tributario con esame ordinato degli atti e delle scadenze.", Image: images["tax-ledger"]},
	}

	pages := map[string]PageData{
		"/": {
			Title: "Studio Legale Alessandro Callegarin", Description: "Assistenza legale per persone, famiglie e patrimoni a Gallarate e in provincia di Varese.", Path: "/", Kind: pageKindHome,
			Eyebrow: "Studio legale", Heading: "Assistenza legale chiara e rigorosa, vicina alle persone e alle loro esigenze.", Lead: "Lo Studio Legale Alessandro Callegarin offre consulenza e assistenza a Gallarate e nel territorio della provincia di Varese, con un approccio fondato sull’ascolto, sulla chiarezza e sulla valutazione concreta di ogni situazione.",
			HeroImage: images["hero-architecture"], ApproachImage: images["approach-library"], ArticleImage: images["article-notebook"], ContactImage: images["contact-entrance"], Areas: areas, HighlightedAreas: areas[:4],
		},
		"/profilo": {
			Title: "Profilo", Description: "Profilo professionale dello Studio Legale Alessandro Callegarin.", Path: "/profilo", Kind: pageKindProfile,
			Eyebrow: "Profilo", Heading: "Alessandro Callegarin", Lead: "Alessandro Callegarin si è laureato in Giurisprudenza presso l’Università degli Studi di Milano nel 2018. Svolge l’attività di avvocato a Gallarate dal 2022.", HeroImage: images["hero-architecture"],
			Sections: []ContentSection{{Heading: "Approccio", Paragraphs: []string{"Ogni questione richiede attenzione, metodo e una valutazione costruita sulle reali esigenze della persona.", "L’obiettivo è offrire indicazioni comprensibili, illustrare con trasparenza le possibili strade e individuare la tutela più appropriata per il caso concreto."}}, {Heading: "Ambiti di attività", Paragraphs: []string{"Lo Studio assiste privati, famiglie e realtà del territorio in materia di diritto civile, penale e tributario. L’attività comprende, in particolare, separazioni e divorzi, tutela delle persone e dei minori, successioni e donazioni, contratti e locazioni, recupero crediti, risarcimento dei danni, diritti reali, procedimenti penali e contenzioso tributario."}}},
		},
		"/approccio": {
			Title: "Approccio", Description: "Principi di lavoro, riservatezza e relazione con il cliente.", Path: "/approccio", Kind: pageKindApproach,
			Eyebrow: "Approccio", Heading: "Comprendere prima di indicare una direzione", Lead: "Ogni questione richiede attenzione, metodo e una valutazione costruita sulle reali esigenze della persona.", HeroImage: images["approach-library"],
			Sections: []ContentSection{{Heading: "Chiarezza e trasparenza", Paragraphs: []string{"L’obiettivo è offrire indicazioni comprensibili, illustrare con trasparenza le possibili strade e individuare la tutela più appropriata per il caso concreto."}}, {Heading: "Riservatezza", Paragraphs: []string{"Le informazioni sono trattate con discrezione e secondo le regole applicabili. Le modalità definitive sono DA VALIDARE CON IL PROFESSIONISTA."}}, {Heading: "Relazione", Paragraphs: []string{"Aggiornamenti e passaggi operativi vengono espressi in modo diretto, senza promettere risultati e senza semplificare ciò che richiede cautela."}}},
		},
		"/aree-di-attivita": {
			Title: "Aree di attività", Description: "Le aree di assistenza legale per persone, famiglie, patrimoni e piccole attività.", Path: "/aree-di-attivita", Kind: pageKindAreas,
			Eyebrow: "Competenze", Heading: "Aree di attività", Lead: "Lo Studio assiste privati, famiglie e realtà del territorio in materia di diritto civile, penale e tributario. L’attività comprende, in particolare, separazioni e divorzi, tutela delle persone e dei minori, successioni e donazioni, contratti e locazioni, recupero crediti, risarcimento dei danni, diritti reali, procedimenti penali e contenzioso tributario.", HeroImage: images["contracts-pen"], Areas: areas,
		},
		"/sentenze-e-riflessioni": {
			Title: "Sentenze e riflessioni", Description: "Approfondimenti su decisioni e temi di diritto.", Path: "/sentenze-e-riflessioni", Kind: pageKindArticles,
			Eyebrow: "Approfondimenti", Heading: "Sentenze e riflessioni", Lead: "Uno spazio editoriale dedicato a decisioni rilevanti e temi di diritto che incidono sulla vita delle persone.", HeroImage: images["article-notebook"],
			Sections: []ContentSection{{Heading: "Pubblicazioni", Paragraphs: []string{"DATO DA CONFERMARE — i contenuti pubblicati saranno mostrati qui dopo la revisione editoriale."}}},
		},
		"/contatti": {
			Title: "Contatti", Description: "Contatti dello Studio Legale Alessandro Callegarin.", Path: "/contatti", Kind: pageKindContact,
			Eyebrow: "Contatti", Heading: "Un primo confronto, con riservatezza", Lead: "Recapiti diretti e modulo per una prima richiesta di contatto.", HeroImage: images["contact-entrance"],
			Sections: []ContentSection{{Heading: "Richiesta di contatto", Paragraphs: []string{"L’invio di una richiesta non costituisce conferimento di incarico."}}},
		},
		"/privacy-cookie-policy": {
			Title: "Privacy e cookie policy", Description: "Informazioni sul trattamento dei dati e sull'uso dei cookie.", Path: "/privacy-cookie-policy", Kind: pageKindPrivacyPolicy,
			Eyebrow: "Informazioni", Heading: "Privacy e cookie policy", Lead: "DA VALIDARE CON IL PROFESSIONISTA — informativa legale di sviluppo, non destinata alla pubblicazione.", HeroImage: images["approach-library"],
			Sections: []ContentSection{
				{Heading: "Sito pubblico", Paragraphs: []string{"Le pagine pubbliche non impostano cookie, non usano strumenti di analisi e non richiedono risorse da servizi terzi. Non è quindi mostrato alcun banner cookie."}},
				{Heading: "Titolare e contatti", Paragraphs: []string{"DA VALIDARE CON IL PROFESSIONISTA — Titolare del trattamento, recapiti e canale per l'esercizio dei diritti: DATO DA CONFERMARE."}},
				{Heading: "Richieste di contatto", Paragraphs: []string{"DA VALIDARE CON IL PROFESSIONISTA — finalità: rispondere alle richieste; base giuridica: misure precontrattuali richieste dall'interessato; destinatari e responsabili: fornitori tecnici necessari, inclusa l'infrastruttura Azure nell'Unione europea, regione Italy North; accesso limitato alle persone autorizzate.", "DA VALIDARE CON IL PROFESSIONISTA — revisione operativa entro 24 mesi senza cancellazione automatica; dopo una richiesta di cancellazione manuale, recuperabilità limitata a 30 giorni prima della rimozione definitiva.", "DA VALIDARE CON IL PROFESSIONISTA — diritti e relativo canale di esercizio: DATO DA CONFERMARE."}},
			},
		},
	}

	areaDetails := []struct {
		index int
		items []string
	}{
		{0, []string{"Separazione e divorzio", "Amministrazione di sostegno", "Procedimenti davanti al tribunale per i minorenni"}},
		{1, []string{"Successioni", "Donazioni", "Dichiarazioni di successione"}},
		{2, []string{"Locazioni", "Compravendita", "Appalto"}},
		{3, []string{"Valutazione del credito", "Fase stragiudiziale", "Azioni giudiziali consentite"}},
		{4, []string{"Circolazione stradale", "Responsabilità medica", "Altre ipotesi di danno"}},
		{5, []string{"Proprietà e usufrutto", "Servitù", "Pegno e ipoteca"}},
		{6, []string{"Reati contro persone e famiglie", "Reati contro il patrimonio", "Reati stradali"}},
		{7, []string{"Esame degli atti", "Valutazione della controversia", "Contenzioso tributario"}},
	}
	for _, detail := range areaDetails {
		area := areas[detail.index]
		pages[area.Href] = PageData{
			Title: area.Title, Description: area.Description, Path: area.Href, Kind: pageKindArea,
			Eyebrow: "Area di attività", Heading: area.Title, Lead: area.Description, HeroImage: area.Image,
			Sections: []ContentSection{{Heading: "Ambiti di assistenza", Items: detail.items}, {Heading: "Valutazione del caso", Paragraphs: []string{"Ogni situazione viene esaminata nei suoi elementi concreti; questa pagina offre informazioni generali e non sostituisce una consulenza."}}},
		}
	}

	for path, page := range pages {
		page.SiteName = "Studio Legale Alessandro Callegarin"
		page.Navigation = navigationFor(path)
		page.Contact = confirmedStudioContact
		pages[path] = page
	}
	return pages
}

func navigationFor(path string) []NavigationItem {
	items := []NavigationItem{
		{Label: "Profilo", Href: "/profilo"},
		{Label: "Aree di attività", Href: "/aree-di-attivita"},
		{Label: "Approccio", Href: "/approccio"},
		{Label: "Sentenze e riflessioni", Href: "/sentenze-e-riflessioni"},
		{Label: "Contatti", Href: "/contatti"},
	}
	for index := range items {
		items[index].Current = path == items[index].Href || (items[index].Href == "/aree-di-attivita" && len(path) > len(items[index].Href) && path[:len(items[index].Href)] == items[index].Href)
	}
	return items
}
