# This is a common script library from an earlier version of the proxy.

import base64
import copy
import json
import socket
import sys
import urllib.request

return_code = 0
# return code 1 means a syntax error was encountered
# return code 2 means requests have been made

def to_bytes(string):
  if sys.version_info.major == 3:
    return bytes(string, 'UTF-8')
  else:
    return bytes(string)

def make_request(host, request, https = False):
  global return_code
  if type(request) is str:
    request_body = request.replace("\n", "\x0D\x0A")
    request_body = bytes(request, 'UTF-8')
  elif type(request) is bytes:
    request_body = request
  else:
    request_body = request.request_body

  b64request = base64.b64encode(request_body)
  request_data = str(b64request, 'UTF-8')

  if type(request) is str or type(request) is bytes:
    properties = {}
  else:
    properties = request.properties

  properties['request'] = request_data
  properties['host']    = host
  properties['ssl']     = https
  properties['scan_id'] = 'SCRIPT_ID'

  if 'script_type' not in properties:
    properties['script_type'] = 'Script'

  property_data = json.dumps(properties)

  return_code = 2
  headers = {
    'X-API-Key': 'API_KEY'
  }

  req = urllib.request.Request("http://localhost:PROXY_PORT/proxy/add_request_to_queue", property_data.encode('UTF-8'), headers)
  with urllib.request.urlopen(req) as call_response:
    response = call_response.read()

class Request:
  def __init__(self, host, ssl, request_body, request_id = None):
    self.host             = host
    self.ssl              = ssl
    self.request_body     = base64.b64decode(request_body)
    self.request_id       = request_id
    self.properties       = {}
    self.inject_payloads  = []
    self.injection_points = []

  def set_inject_payloads(self, inject_payloads):
    self.inject_payloads = inject_payloads

  def set_injection_points(self, injection_points):
    self.injection_points = injection_points

  def injection_point_count(self):
    return len(self.injection_points)

  def set_properties(self, properties):
    self.properties = properties

  def correct_content_length(self):
    idx_cl  = self.request_body.find(bytes('Content-Length: ', 'UTF-8'))
    idx_eoh = self.request_body.find(b'\x0D\x0A\x0D\x0A')
    if idx_cl == -1 or idx_eoh  == -1:
      return self
    self.set_header('Content-Length', str(len(self.request_body) - idx_eoh - 4))
    return self

  def set_header(self, header, value):
    header = to_bytes(header)
    value  = to_bytes(value)
    location = self.request_body.find(header)

    if location == -1:
      header = b'\x0D\x0A' + header
      location = self.request_body.find(b'\x0D\x0A\x0D\x0A')

    end_location = self.request_body.find(b"\r\n", location)
    self.request_body = self.request_body[0:location] + header + b': ' + value + self.request_body[end_location:]

  def replace_injection_point(self, index, replacement):
    injection_offset = self.injection_points[index][0]
    injection_length = self.injection_points[index][1]
    replacement_length_diff = len(replacement) - injection_length
    first_part  = self.request_body[0:injection_offset]
    second_part = self.request_body[injection_offset+injection_length:]

    new_injection_points = copy.deepcopy(self.injection_points)
    new_injection_points[index][0] = injection_offset
    new_injection_points[index][1] = len(replacement)

    # modify the proceeding injection points to reflect their new offsets
    for i in range(index + 1, len(new_injection_points)):
      new_injection_points[i][0] = new_injection_points[i][0] + replacement_length_diff

    new_properties = self.properties
    new_inject_payloads = self.inject_payloads
    new_inject_payloads.append([index, replacement])
    new_properties['inject_payloads'] = json.dumps(new_inject_payloads)

    modified_request_data = base64.b64encode(first_part + bytes(replacement, 'UTF-8') + second_part)
    new_req = Request(self.host, self.ssl, modified_request_data)
    new_req.set_properties(new_properties)
    new_req.set_injection_points(new_injection_points)
    new_req.set_inject_payloads(new_inject_payloads)
    return new_req

  def make(self):
    return make_request(self.host, self, self.ssl)
