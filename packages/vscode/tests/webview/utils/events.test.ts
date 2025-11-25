import { describe, expect, it, vi } from 'vitest';
import { createStoppedEventHandler } from '../../../src/webview/utils/events';

describe('Event utilities', () => {
  describe('createStoppedEventHandler', () => {
    it('stops event propagation', () => {
      const dispatch = vi.fn();
      const event = new Event('click', { bubbles: true });
      event.stopPropagation = vi.fn();

      const handler = createStoppedEventHandler(dispatch, 'customEvent', { foo: 'bar' });
      handler(event);

      // eslint-disable-next-line @typescript-eslint/unbound-method
      const stopPropagation = event.stopPropagation;
      expect(stopPropagation).toHaveBeenCalled();
    });

    it('dispatches custom event with data', () => {
      const dispatch = vi.fn();
      const event = new Event('click');
      const data = { foo: 'bar', baz: 123 };

      const handler = createStoppedEventHandler(dispatch, 'customEvent', data);
      handler(event);

      expect(dispatch).toHaveBeenCalledWith('customEvent', data);
    });

    it('works with different event types', () => {
      const dispatch = vi.fn();
      const data = { value: 'test' };

      const clickHandler = createStoppedEventHandler(dispatch, 'onClick', data);
      const keyHandler = createStoppedEventHandler(dispatch, 'onKey', data);

      clickHandler(new Event('click'));
      keyHandler(new Event('keydown'));

      expect(dispatch).toHaveBeenCalledWith('onClick', data);
      expect(dispatch).toHaveBeenCalledWith('onKey', data);
    });

    it('works without generic type', () => {
      const dispatch = vi.fn();
      const event = new Event('click');

      const handler = createStoppedEventHandler(dispatch, 'test', undefined);
      handler(event);

      expect(dispatch).toHaveBeenCalledWith('test', undefined);
    });
  });
});
