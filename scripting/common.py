import base64
import html
import json
import socket
import sys
from urllib.error import HTTPError
from urllib.parse import urlparse
import urllib.request

def to_bytes(string):
  if sys.version_info.major == 3:
    return bytes(string, 'UTF-8')
  else:
    return bytes(string)

def make_request_to_core(uri, obj = {}):
  property_data = None
  method = 'GET'
  content_type = 'application/json'
  
  if obj != {} and type(obj) is dict:
    obj['scan_id'] = 'PROXIMITY_SCRIPT_ID'
    property_data = json.dumps(obj).encode('UTF-8')
    method = 'POST'
  elif obj != {} and type(obj) is str:
    property_data = obj
    method = 'PATCH'

  headers = {
    'X-API-Key': 'PROXIMITY_API_KEY'
  }

  if property_data is not None and property_data != {}:
    headers['Content-Type'] = content_type

  req = urllib.request.Request("http://localhost:PROXIMITY_PROXY_PORT" + uri, property_data, headers, method=method)
  
  try:
    with urllib.request.urlopen(req) as call_response:
      response = call_response.read()
      return response
  except HTTPError as e:
    content = e.read()
    print("Server returned error: " + content.decode('UTF-8'))
    return None

class GeneratedRequest:
  def __init__(self, request):
    self.request = request
    self.request_body = b''
    for part in request.request_parts:
      self.request_body += base64.standard_b64decode(part['RequestPart'])
    
    # convert from regular newline encodings to crln, if required
    if self.request_body.find(b'\r\n') == -1:
      self.request_body.replace(b'\n', b'\r\n')

  def make(self, queue=True):
    b64request = base64.b64encode(self.request_body)
    request_data = str(b64request, 'UTF-8')

    properties = self.request.properties

    properties['request'] = request_data
    properties['host']    = self.request.host
    properties['ssl']     = self.request.ssl

    url = '/requests/queue'
    if queue == False:
      url = '/requests/make'

    return make_request_to_core(url, properties)

  def get_response(self):
    make_req_response = self.make(False)
    json_response = json.loads(make_req_response)
    guid = json_response['GUID']
  
    return get_response_for_request(guid)

class Request:
  def __init__(self, host, ssl, request_parts):
    self.host             = host
    self.ssl              = ssl
    self.request_parts    = json.loads(request_parts)
    self.properties       = {}

  def generate_request(self):
    return GeneratedRequest(self)

  def injection_point_count(self):
    count = 0
    for part in self.request_parts:
      if part['Inject'] == True:
        count += 1
    
    return count

  def make(self):
    return GeneratedRequest(self).make()

  def make_get_response(self):
    return GeneratedRequest(self).get_response()

  def replace_injection_point(self, index, replacement):
    i = 0
    orig_request_part = ''
    for part in self.request_parts:
      if part['Inject'] == True:
        if i == index:
          orig_request_part = base64.standard_b64decode(part['RequestPart'])
          part['RequestPart'] = base64.standard_b64encode(to_bytes(replacement))
          break
        else:
          i += 1
    
    self.properties['payloads'] = json.dumps({orig_request_part.decode(): replacement})

  def set_properties(self, properties):
    self.properties = properties
