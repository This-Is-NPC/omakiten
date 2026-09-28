package tui

// WireAppServices fills Editor from the BundleCache entry the runtime
// installed at boot. Metrics / Insights / Search bind lazily from
// operation.Service. The CLI composition root calls this so it does not
// import internal/app (D15 / D20).
func WireAppServices(repos Repositories, configPath string) Repositories {
	if repos.Cache != nil {
		if pr := repos.Cache.View(repos.ProjectID); pr != nil {
			if repos.Editor == nil {
				repos.Editor = pr.Editor
			}
		}
	}
	if repos.ConfigPath == "" {
		repos.ConfigPath = configPath
	}
	return repos
}
