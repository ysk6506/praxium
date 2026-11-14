package httpapi

// kgRoutes are the routes that read a KG (rest-api-resources-graph.md 1-1): the
// KGs themselves, and the first three closed queries in their short GET form
// (find, get and neighbors) in their short GET form and with the whole input in a body.
func kgRoutes() []routeSpec {
	return []routeSpec{
		get("/v1/kgs", reader, (*call).kgList),
		get("/v1/kgs/{kg}", reader, (*call).kgOne),
		get("/v1/kgs/{kg}/objects", reader, (*call).objectsFind),
		get("/v1/kgs/{kg}/objects/{object_id}", reader, (*call).objectGet),
		get("/v1/kgs/{kg}/objects/{object_id}/links", reader, (*call).objectLinks),
		post("/v1/kgs/{kg}/queries:find", reader, (*call).askFind),
		post("/v1/kgs/{kg}/queries:get", reader, (*call).askGet),
		post("/v1/kgs/{kg}/queries:neighbors", reader, (*call).askNeighbors),
		post("/v1/kgs/{kg}/queries:impact", reader, (*call).askImpact),
	}
}
