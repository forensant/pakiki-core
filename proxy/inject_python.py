payloads = {{.Payloads}}

request = Request('{{.Host}}', {{.SSL}}, '{{.Request}}')
request.set_properties({'base_request': True})
request.set_injection_points({{.PointList}})
request.make()

for point in range(0, request.injection_point_count()):
    for payload in payloads:
        new_request = copy.deepcopy(request)
        new_request = new_request.replace_injection_point(point, payload)
        new_request.correct_content_length()
        new_request.make()
