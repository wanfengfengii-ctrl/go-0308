import { copyFile, mkdir } from 'node:fs/promises';
import { join } from 'node:path';

const files = ['index.html', 'app.js', 'style.css'];
await mkdir('dist', { recursive: true });
for (const f of files) {
  await copyFile(join('src', f), join('dist', f));
}
console.log('frontend built -> dist/');
