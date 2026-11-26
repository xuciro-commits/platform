import { autocompletion, type CompletionContext } from "@codemirror/autocomplete";
import { setDiagnostics } from "@codemirror/lint";
import { EditorState } from "@codemirror/state";
import { EditorView } from "@codemirror/view";
import { Field } from "@astryxdesign/core/Field";
import { useEffect, useId, useRef, useState } from "react";
import { type Api, request } from "../host";

const words = { formula: /[a-z0-9]*/, default: /[\w:+]*/ };

export type ExpressionSlot = keyof typeof words;

export function useFieldExpressions(fields: unknown[], field: Record<string, unknown>) {
  const [answer, setAnswer] = useState<Api.FieldExpressionDiagnostics>();
  const body = JSON.stringify({ fields, field });
  useEffect(() => {
    const controller = new AbortController();
    const timer = setTimeout(() => {
      request<Api.FieldExpressionDiagnostics>("/v1/build/fields/expressions/check", { body: JSON.parse(body), signal: controller.signal })
        .then(setAnswer, () => undefined);
    }, 250);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [body]);
  return answer;
}

export function ExpressionEditor({ label, description, slot, value, options, issues, onChange }: {
  label: string; description?: string; slot: ExpressionSlot; value: string;
  options: Api.ExpressionOption[]; issues: Api.ExpressionIssue[]; onChange: (value: string) => void;
}) {
  const id = useId();
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView>(undefined);
  const latest = useRef({ options, onChange });
  latest.current = { options, onChange };
  useEffect(() => {
    const complete = (context: CompletionContext) => {
      const word = context.matchBefore(words[slot]);
      if (!word || word.from === word.to && !context.explicit) return null;
      return { from: word.from, options: latest.current.options.map((option) => ({ label: option.label, detail: option.detail })) };
    };
    const editor = new EditorView({
      parent: host.current!,
      state: EditorState.create({
        doc: value,
        extensions: [
          autocompletion({ override: [complete] }),
          EditorView.contentAttributes.of({ id, "aria-label": label }),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) latest.current.onChange(update.state.doc.toString());
          }),
        ],
      }),
    });
    view.current = editor;
    return () => editor.destroy();
  }, [slot, id, label]);
  useEffect(() => {
    const editor = view.current;
    if (editor && editor.state.doc.toString() !== value) {
      editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: value } });
    }
  }, [value]);
  const shown = issues.filter((issue) => issue.slot === slot);
  useEffect(() => {
    const editor = view.current;
    if (!editor) return;
    const length = editor.state.doc.length;
    editor.dispatch(setDiagnostics(editor.state, shown.map((issue) => ({
      from: Math.min(issue.from, length), to: Math.min(Math.max(issue.to, issue.from), length), severity: "error", message: issue.message,
    }))));
  }, [JSON.stringify(shown)]);
  return (
    <Field label={label} description={description} inputID={id}
      status={shown.length ? { type: "error", message: shown.map((issue) => issue.message).join("; ") } : undefined}>
      <div ref={host} />
    </Field>
  );
}
