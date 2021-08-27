payloads = {{.Payloads}}

request = Request('{{.Host}}', {{.SSL}}, '{{.Request}}')
request.set_properties({'base_request': True})
request.make()

for point in range(0, request.injection_point_count()):
    for payload in payloads:
        new_request = copy.deepcopy(request)
        new_request.replace_injection_point(point, payload)

        generated_request = new_request.generate_request()
        generated_request.make()
