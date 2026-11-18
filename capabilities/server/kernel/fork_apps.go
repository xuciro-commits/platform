package kernel

import "platformserver/platform"

func ForkApps(apps []platform.Service) ([]platform.Service, error) {
	forks := make([]platform.Service, len(apps))
	for i, app := range apps {
		ledger, err := app.Authority().ForkRead()
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
