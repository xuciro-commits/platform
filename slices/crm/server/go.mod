module crm

go 1.27.1

require (
	lodging v0.0.0
	platformkernel v0.0.0
	platformserver v0.0.0
)

require google.golang.org/protobuf v1.36.12 // indirect

replace platformkernel => ../../../contract/go

replace platformserver => ../../../capabilities/server

replace lodging => ../../../protocols/lodging
