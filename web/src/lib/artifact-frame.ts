// Agent-written HTML is untrusted. It renders only inside <iframe sandbox="">
// (opaque origin: no scripts, forms, popups, top navigation or same-origin
// access). This policy is defense in depth that also blocks network loads,
// so a page cannot beacon to other hosts; inline styles and data: media work.
export const artifactPolicy =
	"default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; media-src data:; form-action 'none'; base-uri 'none'";

// The policy meta comes first so it applies before any agent markup is parsed;
// an agent's own doctype or head tags merge into this document.
export function artifactDocument(html: string) {
	return `<!doctype html><meta http-equiv="Content-Security-Policy" content="${artifactPolicy}"><meta name="referrer" content="no-referrer">${html}`;
}
