/**
 * Event handler utilities to reduce boilerplate code
 */

/**
 * Creates an event handler that stops propagation and dispatches a custom event
 */
export function createStoppedEventHandler<T = unknown>(
  dispatch: (type: string, detail?: T) => void,
  eventType: string,
  data: T
) {
  return (event: Event) => {
    event.stopPropagation();
    dispatch(eventType, data);
  };
}
