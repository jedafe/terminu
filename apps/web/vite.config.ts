import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		proxy: {
			'/api': 'http://127.0.0.1:7700',
			'/ws': { target: 'ws://127.0.0.1:7700', ws: true }
		}
	}
});
