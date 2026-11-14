package kernel

import "platformserver/platform"

func ForkApps(apps []platform.App) ([]platform.App, error) {
	forks := make([]platform.App, len(apps))
	for i, app := range apps {
		ledger, err := app.AcceptedLedger().ForkRead()
		if err != nil {
			return nil, err
		}
		fork, err := app.Fork(ledger)
		if err != nil {
			return nil, err
		}
		forks[i] = fork
	}
	return forks, nil
}
