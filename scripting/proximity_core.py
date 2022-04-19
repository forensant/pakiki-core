import base64
import html
import json
import socket
import sys
from urllib.error import HTTPError
from urllib.parse import urlparse
import urllib.request

def to_bytes(string: str) -> bytes:
  """Converts the given string to bytes."""
  return bytes(string, 'UTF-8')


def make_request_to_core(uri: str, obj = {}) -> bytes:
  """Makes a request to the core.
  
  Sets the appropriate port, headers, etc.

  :param uri: The URI to request (EG: '/requests').
  :type uri: str
  :param obj: The object to send to the core.
      Can be either a Python dict or a string/bytes. If it's a dict,
      it will be transferred in JSON format. If it's a string/bytes,
      it will be transferred as a POST request body. In that case,
      it should be encoded appropriately beforehand.
  :returns: The response from the core.
  :rtype: bytes
  """
  property_data = None
  method = 'GET'
  content_type = 'application/json'
  
  if obj != {} and type(obj) is dict:
    obj['scan_id'] = 'PROXIMITY_SCRIPT_ID'
    property_data = json.dumps(obj).encode('UTF-8')
    method = 'POST'
  elif obj != {} and type(obj) is str:
    property_data = to_bytes(obj)
    content_type = 'application/x-www-form-urlencoded'
    method = 'POST'
  elif obj != {} and type(obj) is bytes:
    property_data = obj
    content_type = 'application/x-www-form-urlencoded'
    method = 'POST'

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

class InjectableGeneratedRequest:
  # internal class to keep the InjectableRequest class cleaner
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

    url = '/requests/queue'

    return make_request_to_core(url, properties)

  def get_response(self):
    make_req_response = self.make(False)
    json_response = json.loads(make_req_response)
    guid = json_response['GUID']
  
    return get_response_for_request(guid)

class InjectableRequest:
  """A request object which is split into parts which can be replaced with alternative payloads.
  
  :param host: The host to make the request to.
  :type host: str
  :param ssl: Whether or not to use SSL.
  :type ssl: bool
  :param request_parts: A JSON representation of an array of project.InjectOperationRequestParts.
      Each one contains {inject:bool, requestPart:str}
      Where the requestPart is a base64 encoded representation of that part of the request.
  """
  def __init__(self, host: str, ssl:bool, request_parts: str):
    self.host             = host
    self.ssl              = ssl
    self.request_parts    = json.loads(request_parts)
    self.properties       = {}

  def injection_point_count(self) -> int:
    """Counts the number of injection points"""
    count = 0
    for part in self.request_parts:
      if part['Inject'] == True:
        count += 1
    
    return count

  def queue(self):
    """Adds the request to the request queue."""
    return InjectableGeneratedRequest(self).make()

  def replace_injection_point(self, index: int, replacement: str):
    """Replaces the given injection point with the replacement."""
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
    """Sets the properties of the request (which will be passed to the core)."""
    self.properties = properties
