import '@testing-library/jest-dom/vitest';

// jsdom has no scrolling.
window.scrollTo = () => {};
