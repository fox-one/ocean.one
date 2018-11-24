import Polyglot from 'node-polyglot';

function Locale(lang) {
  var locale = 'zh-Hans';
  // disable en locale
  // if (lang && lang.indexOf('en') >= 0) {
  //   locale = 'en-US';
  // }
  this.polyglot = new Polyglot({locale: locale});
  this.polyglot.extend(require('./' + locale + '.json'));
}

Locale.prototype = {
  t: function(key, options) {
    return this.polyglot.t(key, options);
  }
};

export default Locale;
