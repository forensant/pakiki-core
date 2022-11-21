import copy

payloads = {{.Payloads}}

request = InjectableRequest('{{.Host}}', {{.SSL}}, '{{.Request}}')
request.set_properties({'base_request': True})
request.queue()
request.set_properties({})

for point in range(0, request.injection_point_count()):
    for payload in payloads:
        new_request = copy.deepcopy(request)
        new_request.replace_injection_point(point, payload)

        new_request.queue()