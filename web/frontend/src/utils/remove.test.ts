import { describe, expect, it } from 'vitest';
import { removeConfirmText } from './remove';

describe('removeConfirmText', () => {
  it('says the event leaves the dashboard and is not notified again', () => {
    const text = removeConfirmText('Fado Night');
    expect(text).toContain('"Fado Night"');
    expect(text).toContain('from the dashboard');
    expect(text).toContain('will not be notified again');
    expect(text).not.toContain('database');
  });

  it('names an untitled event generically', () => {
    expect(removeConfirmText('')).toContain('this event');
  });
});
