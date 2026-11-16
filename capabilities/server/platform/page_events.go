package platform

func (p Page) RecordVariableObject(variable string) string {
	return p.RecordResourceObject(variable).Name
}
