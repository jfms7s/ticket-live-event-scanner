import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';
import './styles/index.css';

const rootElement = document.getElementById('root');
if (!rootElement) {
  throw new Error('Root element #root not found');
}

if (!window.API_BASE_URL) {
  rootElement.textContent =
    'Configuration error: API_BASE_URL is not defined. Please ensure config.js is loaded correctly.';
} else {
  createRoot(rootElement).render(
    <StrictMode>
      <App />
    </StrictMode>
  );
}
