import {cleanup,fireEvent,render,screen,waitFor} from '@testing-library/react';
import {afterEach,expect,test} from 'vitest';
import {PageHeader} from './PageHeader';
import {Panel} from '../primitives/card';

afterEach(cleanup);
test('task explanations use the same entry without hiding the current status',async()=>{
  render(<><PageHeader title="Application" description="Could not load the selected version." help="Only authorized application resources are included."/><Panel title="Recovery" help="Previous versions remain available."><p role="alert">A retry needs attention.</p></Panel></>);
  expect(screen.getByText('Could not load the selected version.')).toBeTruthy();
  expect(screen.getByRole('alert').textContent).toBe('A retry needs attention.');
  expect(screen.queryByText('Only authorized application resources are included.')).toBeNull();
  const button=screen.getByRole('button',{name:'About Application'});
  fireEvent.click(button);
  expect(await screen.findByRole('dialog',{name:'Application'})).toBeTruthy();
  expect(screen.getByText('Only authorized application resources are included.')).toBeTruthy();
  fireEvent.keyDown(screen.getByRole('dialog'),{key:'Escape'});
  await waitFor(()=>expect(screen.queryByRole('dialog')).toBeNull());
  await waitFor(()=>expect(document.activeElement).toBe(button));
  fireEvent.click(screen.getByRole('button',{name:'About Recovery'}));
  expect(await screen.findByRole('dialog',{name:'Recovery'})).toBeTruthy();
});
