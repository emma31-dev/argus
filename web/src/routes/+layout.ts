// Every page is prerendered to a static HTML shell; all data is fetched in
// the browser from the Argus API. Trailing slashes give /token/index.html
// style output that any static file server resolves.
export const prerender = true;
export const trailingSlash = 'always';
