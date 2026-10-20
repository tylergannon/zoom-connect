import { chromium } from '../web/node_modules/playwright/index.mjs';
import { readFile, writeFile } from 'node:fs/promises';
import assert from 'node:assert/strict';
const proof=JSON.parse(await readFile('ephemeral/scheduled-real-zoom-proof/result.json','utf8'));
const ended=process.argv.includes('--ended');
const browser=await chromium.launch({headless:true});
try {
 const context=await browser.newContext({viewport:{width:1360,height:1000}});
 const login=await context.request.post(proof.origin+'/api/login',{data:{email:process.env.DEMO_LOGIN_EMAIL,password:process.env.DEMO_LOGIN_PASSWORD}});
 assert.equal(login.status(),200);
 const page=await context.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto(proof.origin+'/calls/'+proof.callID);
 await page.getByRole('heading',{name:'Real Zoom verification: pre-start client',exact:true}).waitFor();
 await page.getByRole('button',{name:/^Chat /}).click();
 await page.getByText('Scheduled stream verification: client connected before this meeting started.',{exact:true}).waitFor();
 if(ended) await page.getByText('Ended',{exact:true}).waitFor();
 await page.screenshot({path:'ephemeral/scheduled-real-zoom-proof/'+(ended?'ended.png':'live-chat.png'),fullPage:true,animations:'disabled'});
 const call=await (await context.request.get(proof.origin+'/api/calls/'+proof.callID)).json();
 assert.equal(call.id,proof.callID);if(ended)assert.equal(call.status,'stopped');assert.deepEqual(errors,[]);
 await writeFile('ephemeral/scheduled-real-zoom-proof/'+(ended?'browser-ended.json':'browser.json'),JSON.stringify({callID:call.id,status:call.status,browserErrors:errors,realZoomChatVisible:true},null,2)+'\n');
 console.log('PASS: real Zoom chat appears in the registered call page.');
} finally {await browser.close();}
