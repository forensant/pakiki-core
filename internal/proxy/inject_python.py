# this is done as a script in order to give us flexibility for other inject
# types/patterns in the future, and we needed the ability to run scripts 
# for the pro version anyway

import base64

payloads = {{.Payloads}}

request = InjectableRequest('{{.Host}}', {{.SSL}}, '{{.Request}}')

replacement_payloads = []
for point in range(0, request.injection_point_count()):
    for payload in payloads:
        req_payloads = []
        for arr_point in range(0, request.injection_point_count()):
            if arr_point == point:
                req_payloads.append(base64.b64encode(bytes(payload, 'utf-8')).decode("ascii"))
            else:
                injectPointData = request.injection_point(arr_point)
                req_payloads.append(injectPointData)

        replacement_payloads.append(req_payloads)

request.bulk_queue(replacement_payloads)
