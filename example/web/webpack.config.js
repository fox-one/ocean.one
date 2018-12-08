const path = require('path');
const webpack = require('webpack');
const HtmlWebpackPlugin = require('html-webpack-plugin');
const ExtractTextPlugin = require("extract-text-webpack-plugin");
const CompressionPlugin = require("compression-webpack-plugin");
const ScriptExtHtmlWebpackPlugin = require("script-ext-html-webpack-plugin");
const FaviconsWebpackPlugin = require('favicons-webpack-plugin');

const extractSass = new ExtractTextPlugin({
    filename: "[name]-[hash].css"
});

const webRoot = function (env) {
  if (env === 'production') {
    return 'https://exo.fox.one';
  } else {
    return 'http://localhost:8000';
  }
};

module.exports = {
  entry: {
    app: './src/app.js'
  },

  output: {
    publicPath: '/',
    path: path.resolve(__dirname, 'dist'),
    filename: '[name]-[chunkHash].js'
  },

  resolve: {
    alias: {
      jquery: "jquery/dist/jquery",
      handlebars: "handlebars/dist/handlebars.runtime"
    }
  },

  module: {
    rules: [{
      test: /\.html$/, loader: "handlebars-loader?helperDirs[]=" + __dirname + "/src/helpers"
    }, {
      test: /\.(scss|css)$/,
      use: extractSass.extract({
        use: [{
          loader: "css-loader"
        }, {
          loader: "sass-loader"
        }],
        fallback: "style-loader"
      })
    }, {
      test: /\.(woff|woff2|eot|ttf|otf|svg|png|jpg|gif)$/,
      use: [
        'file-loader'
      ]
    }]
  },

  plugins: [
    new webpack.DefinePlugin({
      PRODUCTION: (process.env.NODE_ENV === 'production'),
      WEB_ROOT: JSON.stringify(webRoot(process.env.NODE_ENV)),
      API_ROOT: JSON.stringify("https://example.ocean.one"),
      ENGINE_ROOT: JSON.stringify("wss://events.ocean.one"),
      APP_NAME: JSON.stringify("F1EX O1 Demo"),
      RECAPTCHA_SITE_KEY: JSON.stringify("6Leo5WkUAAAAACT-jCLijZ1yyFvMMxy_yhoiJa3H")
    }),
    // new CompressionPlugin({
    //   asset: "[path]",
    //   algorithm: "gzip",
    //   test: /\.(js|css)$/,
    //   threshold: process.env.NODE_ENV === 'production' ? 0 : 100000000000000,
    //   minRatio: 1,
    //   deleteOriginalAssets: false
    // }),
    new HtmlWebpackPlugin({
      template: './src/layout.html'
    }),
    new FaviconsWebpackPlugin({
      logo: './src/favicon-512.png',
      prefix: 'icons-[hash]-',
      background: 'rgba(0,0,0,0)'
    }),
    new ScriptExtHtmlWebpackPlugin({
      defaultAttribute: 'async'
    }),
    extractSass
  ]
};
