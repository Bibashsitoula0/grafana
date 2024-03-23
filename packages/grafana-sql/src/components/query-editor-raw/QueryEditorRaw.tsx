import React, { useCallback, useEffect, useRef, useState } from 'react';

import { LanguageDefinition, SQLEditor } from '@grafana/experimental';

import { SQLQuery } from '../../types';
import { Button } from '@grafana/ui';

type Props = {
  query: SQLQuery;
  onChange: (value: SQLQuery, processQuery: boolean) => void;
  children?: (props: { formatQuery: () => void }) => React.ReactNode;
  width?: number;
  height?: number;
  editorLanguageDefinition: LanguageDefinition;
};

export function QueryEditorRaw({ children, onChange, query, width, height, editorLanguageDefinition }: Props) {
  // We need to pass query via ref to SQLEditor as onChange is executed via monacoEditor.onDidChangeModelContent callback, not onChange property
  const queryRef = useRef<SQLQuery>(query);

  useEffect(() => {
    queryRef.current = query;
  }, [query]);

  useEffect(() => {}, []);

  const onRawQueryChange = useCallback(
    (rawSql: string, processQuery: boolean) => {
      const newQuery = {
        ...queryRef.current,
        rawQuery: true,
        rawSql,
      };
      onChange(newQuery, processQuery);
    },
    [onChange]
  );

  const [naturalLang, setNaturalLang] = useState('');
  const [isLoading, setIsLoading] = useState(false);

  const fetchRawQuery = async () => {
    if (isLoading) return;
    setIsLoading(true);
    const requestBody = {
      text: naturalLang,
    };

    try {
      const response = await fetch('https://nl2sql-api.turboline.ai/predict', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(requestBody),
      });

      const data = await response.json();
      const newQuery = {
        ...queryRef.current,
        rawQuery: true,
        rawSql: data,
      };
      onChange(newQuery, true);
      query.rawSql = data;
    } catch (error) {
      console.error('Error:', error);
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div style={{ display: 'flex' }}>
      <div style={{ width: '50%' }}>
        <textarea
          placeholder="Write natural language here..."
          className="view-lines monaco-mouse-cursor-text"
          style={{ width: '100%', height: '90%', background: '#111217' }}
          value={naturalLang}
          onChange={(e) => setNaturalLang(e.target.value)}
        ></textarea>

        <Button style={{ float: 'right' }} onClick={() => fetchRawQuery()} variant="primary" size="sm">
          {isLoading ? 'Processing...' : 'Process'}
        </Button>
      </div>

      <div style={{ width: '50%' }}>
        <SQLEditor
          width={width}
          height={height}
          query={query.rawSql!}
          onChange={onRawQueryChange}
          language={editorLanguageDefinition}
        >
          {children}
        </SQLEditor>
      </div>
    </div>
  );
}
