# This is a common script library from an earlier version of the proxy.

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
  
  if obj != {}:
    obj['scan_id'] = 'SCRIPT_ID'
    property_data = json.dumps(obj).encode('UTF-8')

  headers = {
    'X-API-Key': 'API_KEY'
  }

  req = urllib.request.Request("http://localhost:PROXY_PORT" + uri, property_data, headers)
  
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

def get_response_for_request(guid):
  # retrieve and interpret the result
  request_response = make_request_to_core("/project/requestresponse?guid=" + guid)
  request_response = json.loads(request_response)
  request_response['Request']  = base64.b64decode(request_response['Request'])
  request_response['Response'] = base64.b64decode(request_response['Response'])

  response_body_split = request_response['Response'].split(b'\r\n\r\n')
  body = ''
  if len(response_body_split) == 2:
    body = response_body_split[1]

  request_response['ResponseBody'] = body
  return request_response

def make_request_to_url(url):
  url = urlparse(url)
  request = "GET " + url.path + " HTTP/1.1\nHost: " + url.netloc + "\n\n"

  # make the initial request to the URL
  obj = {
    'host': url.netloc,
    'ssl': url.scheme == 'https',
    'request': base64.b64encode(to_bytes(request)).decode("utf-8") 
  }
  make_req_response = make_request_to_core("/proxy/make_request", obj)
  json_response = json.loads(make_req_response)
  guid = json_response['GUID']
  
  return get_response_for_request(guid)

def print_html(html):
  output_obj = {
    'GUID':       'SCRIPT_ID',
    'OutputHTML': html
  }

  make_request_to_core('/project/script/append_html_output', output_obj)

def report_progress(count, total):
  report = {
    'Count': count,
    'Total': total,
    'GUID': 'SCRIPT_ID'
  }

  make_request_to_core('/scripts/update_progress', report)