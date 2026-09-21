package playa

func buildConfiguration(siteLogo *string) Configuration {
	return Configuration{
		SiteName:         "stash-vr",
		SiteLogo:         siteLogo,
		Auth:             false,
		AuthByCode:       false,
		Actors:           true,
		Categories:       true,
		CategoriesGroups: true,
		Studios:          true,
		Scripts:          true,
		Analytics:        false,
		Masks:            false,
	}
}
