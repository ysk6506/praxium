package httpapi_test

func init() {
	pathSamples["kg"] = "main"
	pathSamples["object_id"] = sampleRackID
	requestSamples["POST /v1/kgs/{kg}/questions/get"] = sampleBody(`{"object_id":"` + sampleRackID + `","include":["attributes","link_counts"]}`)
	requestSamples["POST /v1/kgs/{kg}/questions/neighbors"] = sampleBody(`{"object_id":"` + sampleRackID + `","direction":"in","max_steps":2,"shape":"items"}`)
	requestSamples["POST /v1/kgs/{kg}/questions/find"] = sampleBody(`{"object_type":"Rack","limit":2,"shape":"items"}`)
}

func sampleBody(body string) sampleSpec { return sampleSpec{Body: body} }
