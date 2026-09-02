import {readFile,writeFile} from 'node:fs/promises';
const path=new URL('../dist/index.html',import.meta.url);
const html=await readFile(path,'utf8');
const fixed=html.replace('<script type="module" crossorigin','<script defer');
if(fixed===html)throw new Error('Vite module entry was not found in dist/index.html');
await writeFile(path,fixed);
