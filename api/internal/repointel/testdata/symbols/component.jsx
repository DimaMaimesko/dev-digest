import React from 'react';

export function Button({ label }) {
  return <button>{label}</button>;
}

const Card = (props) => <div>{props.children}</div>;

export default class Page extends React.Component {
  render() {
    return <Card />;
  }
}
