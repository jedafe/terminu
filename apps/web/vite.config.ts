import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// Ref: .teos/conventions/env-vars-catalog.md — TU_WEB_DEV_* knobs let a
// remote dev instance retarget the API proxy / widen allowedHosts without
// editing this file. Defaults preserve the local loopback flow.
const apiOrigin = process.env.TU_WEB_DEV_API_ORIGIN ?? 'http://127.0.0.1:7700';
const allowedHosts = (process.env.TU_WEB_DEV_ALLOWED_HOSTS ?? '')
	.split(',')
	.map((h) => h.trim())
	.filter(Boolean);

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		...(allowedHosts.length ? { allowedHosts } : {}),
		proxy: {
			'/api': apiOrigin,
			'/ws': { target: apiOrigin.replace(/^http/, 'ws'), ws: true }
		}
	}
});
