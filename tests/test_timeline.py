from base import BaseTestCase, Timeline
from parameterized import parameterized

normal = [['jack'], ['elonmusk']]

after = [['jack', '1681686036294803456'],
         ['elonmusk', '1681686036294803456']]

no_more = [['mobile_test_8?cursor=DAABCgABF4YVAqN___kKAAICNn_4msIQAAgAAwAAAAIAAA']]

empty = [['emptyuser'], ['mobile_test_10']]

protected = [['mobile_test_7'], ['Empty_user']]

photo_rail = [['mobile_test', ['Bo0nDsYIYAIjqVn', 'BoQbwJAIUAA0QCY', 'BoQbRQxIIAA3FWD', 'Bn8Qh8iIIAABXrG']]]


class TweetTest(BaseTestCase):
    @parameterized.expand(normal)
    def test_timeline(self, username):
        self.open_nitter(username)
        self.assert_element_present(Timeline.older)
        self.assert_element_absent(Timeline.newest)
        self.assert_element_absent(Timeline.end)
        self.assert_element_absent(Timeline.none)

    @parameterized.expand(after)
    def test_after(self, username, cursor):
        self.open_nitter(f'{username}?cursor={cursor}')
        self.assert_element_present(Timeline.newest)
        self.assert_element_present(Timeline.older)
        self.assert_element_absent(Timeline.end)
        self.assert_element_absent(Timeline.none)

    @parameterized.expand(no_more)
    def test_no_more(self, username):
        self.open_nitter(username)
        self.assert_text('No more items', Timeline.end)
        self.assert_element_present(Timeline.newest)
        self.assert_element_absent(Timeline.older)

    @parameterized.expand(empty)
    def test_empty(self, username):
        self.open_nitter(username)
        self.assert_text('No items found', Timeline.none)
        self.assert_element_absent(Timeline.newest)
        self.assert_element_absent(Timeline.older)
        self.assert_element_absent(Timeline.end)

    @parameterized.expand(protected)
    def test_protected(self, username):
        self.open_nitter(username)
        self.assert_text('This account\'s tweets are protected.', Timeline.protected)
        self.assert_element_absent(Timeline.newest)
        self.assert_element_absent(Timeline.older)
        self.assert_element_absent(Timeline.end)

    def test_media_view_tabs(self):
        self.open_nitter('mobile_test/media')
        self.assert_element_present(Timeline.media_view_tabs)
        self.assert_text('Timeline', Timeline.media_view_timeline)
        self.assert_text('Grid', Timeline.media_view_grid)
        self.assert_text('Gallery', Timeline.media_view_gallery)
        self.assert_text('Timeline', Timeline.media_view_active)

    def test_media_view_grid_tab(self):
        self.open_nitter('mobile_test/media?view=grid')
        self.assert_element_present(Timeline.grid_view)
        self.assert_text('Grid', Timeline.media_view_active)

    def test_media_view_gallery_tab(self):
        self.open_nitter('mobile_test/media?view=gallery')
        self.assert_element_present(Timeline.gallery_view)
        self.assert_text('Gallery', Timeline.media_view_active)

    def test_media_view_tabs_not_on_posts(self):
        self.open_nitter('mobile_test')
        self.assert_element_absent(Timeline.media_view_tabs)

    #@parameterized.expand(photo_rail)
    #def test_photo_rail(self, username, images):
        #self.open_nitter(username)
        #self.assert_element_visible(Timeline.photo_rail)
        #for i, url in enumerate(images):
            #img = self.get_attribute(Timeline.photo_rail + f' a:nth-child({i + 1}) img', 'src')
            #self.assertIn(url, img)


class ArticlesTabTest(BaseTestCase):
    def test_articles_tab_on_profile(self):
        self.open_nitter('satyanadella')
        self.assert_element_present('.tab .tab-item a[href="/satyanadella/articles"]')

    def test_articles_timeline(self):
        self.open_nitter('satyanadella/articles')
        self.assert_text('Articles', '.tab .tab-item.active a')
        self.assert_element_present('.timeline .article-card')
        self.assert_element_present('.timeline .article-card a[href^="/i/article/"]')

    def test_articles_card_cover_and_no_raw_link(self):
        self.open_nitter('jack/articles')
        self.assert_element_present('.timeline .article-card .card-image img')
        # the article link tweet text is redundant with the card and is stripped
        self.assert_element_absent('.timeline .tweet-content a[href*="x.com/i/article"]')

    def test_articles_empty(self):
        self.open_nitter('mobile_test/articles')
        self.assert_text('No items found', Timeline.none)

    def test_articles_multi_user_unsupported(self):
        self.open_nitter('jack,satyanadella')
        self.assert_element_absent('.tab .tab-item a[href$="/articles"]')
        self.open_nitter('jack,satyanadella/articles')
        self.assert_text('Page not found')
