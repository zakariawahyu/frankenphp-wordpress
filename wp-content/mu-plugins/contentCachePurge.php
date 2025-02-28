<?php 
/**
 * Plugin Name:     Content Cache Purge
 * Author:          Stephen Miracle
 * Description:     Purge the content on publish.
 * Version:         0.1.0
 *
 */


 add_action("save_post", function ($id) {
    $post = get_post($id);
    $url = get_site_url() . $_SERVER["PURGE_PATH"] . "/" . $post->post_name . "/";
    wp_remote_post($url, [
        "headers" => [
            "X-WPSidekick-Purge-Key" => $_SERVER["PURGE_KEY"],
        ]
    ]);
 });