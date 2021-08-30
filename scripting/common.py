# This is a common script library from an earlier version of the proxy.

import base64
import copy
import json
import socket
import sys
import urllib.request

def to_bytes(string):
  if sys.version_info.major == 3:
    return bytes(string, 'UTF-8')
  else:
    return bytes(string)

def make_request_to_core(uri, obj):
  obj['scan_id'] = 'SCRIPT_ID'

  property_data = json.dumps(obj)
  headers = {
    'X-API-Key': 'API_KEY'
  }

  req = urllib.request.Request("http://localhost:PROXY_PORT" + uri, property_data.encode('UTF-8'), headers)
  with urllib.request.urlopen(req) as call_response:
    response = call_response.read()


class GeneratedRequest:
  def __init__(self, request):
    self.request = request
    self.request_body = b''
    for part in request.request_parts:
      self.request_body += base64.standard_b64decode(part['RequestPart'])
    
    # convert from regular newline encodings to crln, if required
    if self.request_body.find(b'\r\n') == -1:
      self.request_body.replace(b'\n', b'\r\n')

  def make(self):
    b64request = base64.b64encode(self.request_body)
    request_data = str(b64request, 'UTF-8')

    properties = self.request.properties

    properties['request'] = request_data
    properties['host']    = self.request.host
    properties['ssl']     = self.request.ssl

    make_request_to_core('/proxy/add_request_to_queue', properties)

class Request:
  def __init__(self, host, ssl, request_parts):
    self.host             = host
    self.ssl              = ssl
    self.request_parts    = json.loads(request_parts)
    self.properties       = {}
    self.inject_payloads  = []

  def generate_request(self):
    return GeneratedRequest(self)

  def injection_point_count(self):
    count = 0
    for part in self.request_parts:
      if part['Inject'] == True:
        count += 1
    
    return count

  def make(self):
    GeneratedRequest(self).make()

  def replace_injection_point(self, index, replacement):
    i = 0
    for part in self.request_parts:
      if part['Inject'] == True:
        if i == index:
          part['RequestPart'] = base64.standard_b64encode(to_bytes(replacement))
        else:
          i += 1

    new_properties = self.properties
    new_inject_payloads = self.inject_payloads
    new_inject_payloads.append([index, replacement])
    new_properties['inject_payloads'] = json.dumps(new_inject_payloads)

  def set_properties(self, properties):
    self.properties = properties

def report_progress(count, total):
  report = {
    'Count': count,
    'Total': total,
    'GUID': 'SCRIPT_ID'
  }

  make_request_to_core('/scripts/update_progress', report)