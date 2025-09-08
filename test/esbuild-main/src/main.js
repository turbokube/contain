import { serve } from '@hono/node-server';
import { Hono } from 'hono';

const app = new Hono();
app.get('/', (c) => c.text('from a https://github.com/turbokube/contain test image\n'));

serve(app, (info) => {
  console.log(`Listening on http://localhost:${info.port}`);
});
