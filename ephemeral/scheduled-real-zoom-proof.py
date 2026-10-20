import datetime,json,os,pathlib,time,urllib.request
base='https://zoom-connect-tpg-demo.fly.dev'
out=pathlib.Path('ephemeral/scheduled-real-zoom-proof');out.mkdir(exist_ok=True)
key=os.environ['ZOOM_CONNECT_KEY']
def api(method,path,data=None):
 req=urllib.request.Request(base+path,headers={'Authorization':'Bearer '+key,'Content-Type':'application/json'},data=json.dumps(data).encode() if data is not None else None,method=method)
 with urllib.request.urlopen(req,timeout=20) as response:return json.load(response)
call=api('POST','/api/calls',{'invitation':'https://us06web.zoom.us/j/89180298510','topic':'Real Zoom verification: pre-start client','scheduled_at':'2026-10-20T01:00:00Z'})
assert call['status']=='scheduled' and call['started'] is None
state={'origin':base,'callID':call['id'],'meetingID':'89180298510','clientConnectedBeforeStart':False,'initialStatus':call['status'],'events':[],'realZoom':True}
def save(): (out/'result.json').write_text(json.dumps(state,indent=2)+'\n')
save();print(json.dumps({'callID':call['id'],'url':base+'/calls/'+call['id'],'status':'registered'}),flush=True)
req=urllib.request.Request(base+'/api/calls/'+call['id']+'/stream',headers={'Authorization':'Bearer '+key})
with urllib.request.urlopen(req,timeout=30) as response:
 state['clientConnectedBeforeStart']=True;state['clientConnectedAt']=datetime.datetime.now(datetime.timezone.utc).isoformat();save()
 print('CLIENT CONNECTED BEFORE START; ready for the Zoom meeting.',flush=True)
 event_type='';data=[];seq=None;deadline=time.monotonic()+1200
 raw_stream=(out/'client-stream.txt').open('w')
 for raw in response:
  line=raw.decode().rstrip('\r\n')
  raw_stream.write(line+'\n');raw_stream.flush()
  if time.monotonic()>deadline: raise RuntimeError('Timed out waiting for real Zoom validation')
  if line.startswith('event:'):event_type=line[6:].strip()
  elif line.startswith('id:'):seq=int(line[3:].strip())
  elif line.startswith('data:'):data.append(line[5:].strip())
  elif line=='' and event_type:
   body=json.loads('\n'.join(data) or '{}');state['events'].append({'kind':event_type,'seq':seq,'receivedAt':datetime.datetime.now(datetime.timezone.utc).isoformat()})
   if event_type=='stream.started':state['liveTransition']=True;print('REAL ZOOM STREAM STARTED on the pre-connected client.',flush=True)
   if event_type=='transcript':
    state['transcriptCount']=state.get('transcriptCount',0)+1
    if 'firstTranscript' not in state:state['firstTranscript']=body.get('data',{}).get('text','')[:240];print('FIRST REAL TRANSCRIPT RECEIVED.',flush=True)
   if event_type=='end':state['ended']=True;save();break
   save();event_type='';data=[];seq=None
raw_stream.close()
print(json.dumps(state,indent=2),flush=True)
