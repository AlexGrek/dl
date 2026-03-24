import { marked } from 'marked';

marked.setOptions({ breaks: true, gfm: true });

interface Props {
  content: string;
  class?: string;
}

export function Markdown({ content, class: className }: Props) {
  const html = marked.parse(content) as string;
  return (
    <div
      class={`md-content${className ? ' ' + className : ''}`}
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}
