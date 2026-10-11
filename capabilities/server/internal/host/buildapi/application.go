package buildapi

const (
	AppType        = "build.app"
	SchemaHandOver = AppType + ".publish"
	ValuePagesHelp = "The pages it holds, by their name, in the order people see them"
)

// Group is one heading in an application's navigation.
type Group struct {
	Title string   `json:"title" field:"required" title:"Heading"`
	Pages []string `json:"pages" title:"Pages" help:"Pages of the application, by their name, in their order"`
}
