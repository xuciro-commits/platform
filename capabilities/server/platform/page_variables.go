package platform

func (d PageInputSchema) RawProfile() string { return d.profile }

func PageInputSchemaFor(profile string, variables map[string]PageVariable, queries map[string]PageQuery) PageInputSchema {
	return PageInputSchema{profile: profile, Variables: variables, Queries: queries}
}
