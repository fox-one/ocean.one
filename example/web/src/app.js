import 'simple-line-icons/scss/simple-line-icons.scss';
import './layout.scss';
import $ from 'jquery';
import Navigo from 'navigo';
import Locale from './locale';
import API from './api';
import Auth from './auth';
import Market from './market';
import Account from './account';

const PartialLoading = require('./loading.html');
const Error404 = require('./404.html');
const router = new Navigo(WEB_ROOT, true, '#!');
const api = new API(router, API_ROOT, ENGINE_ROOT);

window.i18n = new Locale(navigator.language);

router.replace = function(url) {
  this.resolve(url);
  this.pause(true);
  this.navigate(url);
  this.pause(false);
};

router.hooks({
  before: function(done, params) {
    $('body').attr('class', 'loading layout');
    $('#layout-container').html(PartialLoading());
    $('title').html(APP_NAME);
    done(true);
  },
  after: function(params) {
    router.updatePageLinks();
  }
});

router.on({
  '/': function () {
    new Market(router, api).index();
  },
  '/auth/callback': function () {
    new Auth(router, api).render();
  },
  '/trade/:market': function (params) {
    new Market(router, api).index(params['market']);
  },
  '/users/new': function () {
    new Account(router, api).signUp();
  },
  '/sessions/new': function () {
    new Account(router, api).signIn();
  },
  '/passwords/new': function () {
    new Account(router, api).resetPassword();
  },
  '/accounts': function () {
    new Account(router, api).assets();
  },
  '/accounts/:id/deposit': function (params) {
    new Account(router, api).asset(params['id'], 'DEPOSIT');
  },
  '/accounts/:id/withdrawal': function (params) {
    new Account(router, api).asset(params['id'], 'WITHDRAWAL');
  },
  '/orders/:market': function (params) {
    new Account(router, api).orders(params['market']);
  }
}).notFound(function () {
  $('#layout-container').html(Error404());
  $('body').attr('class', 'error layout');
  router.updatePageLinks();
}).resolve();

$('body').on('click', '.nav-item.signout', function () {
  api.account.clear();
  window.location.href = '/';
});
