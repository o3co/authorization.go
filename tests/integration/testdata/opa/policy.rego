package authz

default allow := false

allow if {
	input.action == "read"
	startswith(input.resource, "posts/")
}
