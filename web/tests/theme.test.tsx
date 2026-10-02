import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { ThemeSwitch } from '@/layouts/Shell';
import { initTheme, setTheme } from '@/theme';

describe('theme switch', () => {
  afterEach(() => setTheme('system'));

  it('applies and remembers the chosen theme', () => {
    initTheme();
    render(<ThemeSwitch />);
    expect(screen.getByRole('radio', { name: 'System theme' })).toHaveAttribute('aria-checked', 'true');

    fireEvent.click(screen.getByRole('radio', { name: 'Dark theme' }));
    expect(document.documentElement).toHaveClass('dark');
    expect(localStorage.getItem('dth.theme')).toBe('dark');
    expect(screen.getByRole('radio', { name: 'Dark theme' })).toHaveAttribute('aria-checked', 'true');

    fireEvent.click(screen.getByRole('radio', { name: 'Light theme' }));
    expect(document.documentElement).not.toHaveClass('dark');

    fireEvent.click(screen.getByRole('radio', { name: 'System theme' }));
    expect(localStorage.getItem('dth.theme')).toBeNull();
  });
});
