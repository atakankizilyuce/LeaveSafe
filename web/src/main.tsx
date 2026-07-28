import { render } from 'preact';
import { App } from './app';
import './styles/global.css';
import './styles/panel.css';

const root = document.getElementById('app');
if (root) {
    render(<App />, root);
}
