// Demo transport: private R2 objects, authenticated uploads, ten-minute signed GETs.
const encoder = new TextEncoder();
export default {
 async fetch(request, env) {
  if (!env.MEDIA_TOKEN) return new Response('Media not configured', {status:503});
  const url = new URL(request.url);
  const key = url.pathname.slice(1);
  if (!/^screens\/[A-Z0-9]+\/[A-Z0-9]+\.jpg$/.test(key)) return new Response('Not found',{status:404});
  if (request.method === 'PUT') {
   if (request.headers.get('Authorization') !== `Bearer ${env.MEDIA_TOKEN}`) return new Response('Unauthorized',{status:401});
   if (Number(request.headers.get('Content-Length')) > 4194304) return new Response('Too large',{status:413});
   const bytes = await request.arrayBuffer();
   if (bytes.byteLength > 4194304) return new Response('Too large',{status:413});
   await env.IMAGES.put(key, bytes, {httpMetadata:{contentType:'image/jpeg'}});
   return new Response(null,{status:201});
  }
  if (request.method !== 'GET') return new Response('Method not allowed',{status:405});
  const expires = Number(url.searchParams.get('expires'));
  const signature = url.searchParams.get('signature') || '';
  const now = Math.floor(Date.now()/1000);
  if (!Number.isInteger(expires) || expires <= now || expires > now+600 || !/^[a-f0-9]{64}$/.test(signature)) return new Response('Expired or invalid link',{status:403});
  const hmacKey = await crypto.subtle.importKey('raw',encoder.encode(env.MEDIA_TOKEN),{name:'HMAC',hash:'SHA-256'},false,['verify']);
  const signatureBytes = new Uint8Array(signature.match(/../g).map(pair=>parseInt(pair,16)));
  if (!await crypto.subtle.verify('HMAC',hmacKey,signatureBytes,encoder.encode(`${key}\n${expires}`))) return new Response('Invalid link',{status:403});
  const object = await env.IMAGES.get(key);
  if (!object) return new Response('Not found',{status:404});
  return new Response(object.body,{headers:{'Content-Type':'image/jpeg','Cache-Control':'private, no-store','Access-Control-Allow-Origin':'*','X-Content-Type-Options':'nosniff'}});
 }
};
