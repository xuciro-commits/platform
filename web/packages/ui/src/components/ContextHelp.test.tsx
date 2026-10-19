import {cleanup,fireEvent,render,screen,waitFor,within} from '@testing-library/react';
import {afterEach,expect,test} from 'vitest';
import {useState} from 'react';
import {PageHeader} from './PageHeader';
import {ContextHelp} from './ContextHelp';
import {Panel} from '../primitives/card';
import {Dialog} from '../primitives/dialog';
import {Sheet} from '../primitives/sheet';
import {Input} from '../primitives/input';

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

for(const Frame of [Dialog,Sheet])test(`${Frame.name} keeps its draft when nested help closes`,async()=>{
  function Example(){
    const [open,setOpen]=useState(true);
    return <Frame open={open} onOpenChange={setOpen} title="Edit record"><Input aria-label="Record name" defaultValue=""/><ContextHelp title="Name">Use lowercase letters and numbers.</ContextHelp></Frame>;
  }
  render(<Example/>);
  const input=await screen.findByLabelText('Record name');
  fireEvent.change(input,{target:{value:'unsaved'}});
  const trigger=screen.getByRole('button',{name:'About Name'});
  fireEvent.click(trigger);
  const help=await screen.findByRole('dialog',{name:'Name'});
  const close=within(help).getByRole('button',{name:'Close'});
  fireEvent.pointerDown(close,{pointerType:'mouse',button:0});
  fireEvent.click(close);
  await waitFor(()=>expect(screen.queryByRole('dialog',{name:'Name'})).toBeNull());
  expect(screen.getByRole('dialog',{name:'Edit record'})).toBeTruthy();
  expect((input as HTMLInputElement).value).toBe('unsaved');
  await waitFor(()=>expect(document.activeElement).toBe(trigger));
  fireEvent.click(trigger);
  fireEvent.keyDown(await screen.findByRole('dialog',{name:'Name'}),{key:'Escape'});
  await waitFor(()=>expect(screen.queryByRole('dialog',{name:'Name'})).toBeNull());
  expect(screen.getByRole('dialog',{name:'Edit record'})).toBeTruthy();
  expect((input as HTMLInputElement).value).toBe('unsaved');
});
