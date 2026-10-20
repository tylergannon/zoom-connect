import json,os,pathlib,urllib.request
out=pathlib.Path('ephemeral/scheduled-real-zoom-proof')
result=json.loads((out/'result.json').read_text())
base=result['origin'];cid=result['callID']
def get(path,headers=None):
 req=urllib.request.Request(base+path,headers={'Authorization':'Bearer '+os.environ['ZOOM_CONNECT_KEY'],**(headers or {})})
 with urllib.request.urlopen(req,timeout=20) as response:return response.read().decode()
call=json.loads(get('/api/calls/'+cid));events=json.loads(get('/api/calls/'+cid+'/events'))
assert call['status']=='stopped' and call['ended']
assert [e['seq'] for e in events]==list(range(1,len(events)+1))
assert [e['kind'] for e in events]==['call.scheduled','stream.started','chat','stream.paused','stream.stopped']
assert events[2]['data']['text']=='Scheduled stream verification: client connected before this meeting started.'
replay=get('/api/calls/'+cid+'/stream?from=0',{'Last-Event-ID':'3'})
assert 'event: end' in replay and 'event: stream.stopped' in replay
assert 'event: chat' not in replay and 'event: call.scheduled' not in replay
assert result['clientConnectedAt']<result['events'][1]['receivedAt'] and result['ended']
assert result.get('transcriptCount',0)==0
summary={'callID':cid,'realZoom':True,'sameClientConnectedBeforeStart':True,'startChatStopEndOnSameConnection':True,'persistedStatus':call['status'],'persistedEventCount':len(events),'contiguousSequences':True,'lastEventIDReplay':True,'realSpeechCaptureVerified':False,'speechAttempt':'Generated phrase playback attempted; Zoom desktop became unresponsive when starting computer-audio sharing. No transcript received.','zoomMeetingEnded':True,'health':json.loads(get('/healthz'))}
(out/'final.json').write_text(json.dumps(summary,indent=2)+'\n')
(out/'replay-after-3.txt').write_text(replay)
print(json.dumps(summary,indent=2))
