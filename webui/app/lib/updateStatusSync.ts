// Keep status reads ordered across polling and explicit update actions.
export function createUpdateStatusSync<T>(read: () => Promise<T>, apply: (value: T) => void) {
  let generation = 0;
  let disposed = false;
  let pending: Promise<void> | null = null;
  let commandPending = false;
  let commandFailed = false;

  function refresh(): Promise<void> {
    if (disposed || commandPending || commandFailed) return Promise.resolve();
    if (pending) return pending;
    const expected = generation;
    const task = Promise.resolve().then(read).then((value) => {
      if (!disposed && expected === generation) apply(value);
    }).finally(() => {
      if (pending === task) pending = null;
    });
    pending = task;
    return task;
  }

  async function command(action: () => Promise<unknown>, optimistic: T): Promise<void> {
    if (disposed || commandPending) return;
    const expected = ++generation;
    pending = null;
    commandPending = true;
    commandFailed = false;
    apply(optimistic);
    try {
      await action();
      commandPending = false;
      if (!disposed && expected === generation) await refresh();
    } catch (error) {
      // Keep an explicit action failure visible instead of restoring the
      // previous successful check on the next background poll.
      commandFailed = true;
      throw error;
    } finally {
      commandPending = false;
    }
  }

  return {
    refresh, command,
    dispose() { disposed = true; generation++; pending = null; },
  };
}
