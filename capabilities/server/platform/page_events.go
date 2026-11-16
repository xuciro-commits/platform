package platform

func (b PageEventBinding) Navigation() *PageNavigation {
	for _, effect := range b.Effects {
		if effect.Kind == "navigate" {
			return effect.Navigate
		}
	}
	return nil
}

func (p Page) RecordVariableObject(variable string) string {
	return p.RecordResourceObject(variable).Name
}
