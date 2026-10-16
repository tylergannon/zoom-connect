import os,urllib.request as U,urllib.error as E,urllib.parse as P,http.cookiejar,json,time,hashlib,threading,pathlib
base=os.getenv('PROOF_BASE','http://127.0.0.1:8096')
jar=http.cookiejar.CookieJar();client=U.build_opener(U.HTTPCookieProcessor(jar))
def req(path,data=None,headers=None):
 if isinstance(data,dict):data=json.dumps(data).encode();headers=dict(headers or {},**{'Content-Type':'application/json'})
 headers=dict(headers or {})
 if base.startswith('https://'):headers['Authorization']='Bearer '+os.environ['ZOOM_CONNECT_KEY']
 return client.open(U.Request(base+path,data=data,headers=headers))
if not base.startswith('https://'):
 with req('/api/demo/login',{'email':'dana@acme.co'}) as r: assert r.status==200
with req('/api/demo/calls',{'topic':'Screen image transport proof (sample image)','source':'zoom'}) as r:call=json.load(r)
path='/api/calls/'+call['id'];result={}
def listen():
 with req(path+'/stream') as r:
  for raw in r:
   if raw.startswith(b'data: '):
    e=json.loads(raw[6:])
    if e.get('kind')=='screen.frame':result['event']=e;return
thread=threading.Thread(target=listen,daemon=True);thread.start()
jpeg=pathlib.Path('ephemeral/zoom-setup/screenshots/09-live-transcript.jpg').read_bytes()
with req('/api/demo/calls/'+call['id']+'/screenshots',jpeg,{'Content-Type':'image/jpeg','X-Zoom-Timestamp':str(int(time.time()*1000)),'X-Zoom-User-Name':'Sample image - transport proof'}) as r:uploaded=json.load(r);assert r.status==201
thread.join(10);assert 'event' in result,'No live SSE frame'
e=result['event'];url=e['data']['url'];q=P.parse_qs(P.urlsplit(url).query)
assert 599<=int(q['expires'][0])-int(time.time())<=600
with U.urlopen(U.Request(url,headers={'User-Agent':'Mozilla/5.0 ZoomConnectDemo'})) as r: downloaded=r.read();assert r.status==200
assert downloaded==jpeg
for target in [url.split('?')[0],url[:-1]+('0' if url[-1]!='0' else '1')]:
 try:U.urlopen(U.Request(target,headers={'User-Agent':'Mozilla/5.0 ZoomConnectDemo'}));raise AssertionError('unsigned/tampered link accepted')
 except E.HTTPError as ex:assert ex.code==403
with req(path+'/events') as r:history=json.load(r)
assert any(x['kind']=='screen.frame' and x['data'].get('url') for x in history)
proof={'call_id':call['id'],'call_url':base+'/calls/'+call['id'],'uploaded_bytes':len(jpeg),'sha256':hashlib.sha256(jpeg).hexdigest(),'r2_download_matches':True,'sse_frame_received':True,'url_ttl_seconds':600,'unsigned_and_tampered_rejected':True,'source':'existing sample screenshot; not a new Zoom screen-share capture'}
pathlib.Path('ephemeral/shared-screen/'+('hosted-' if base.startswith('https://') else '')+'transport-proof.json').write_text(json.dumps(proof,indent=2))
print(json.dumps(proof))
